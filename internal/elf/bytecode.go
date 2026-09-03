package elf

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/geg971509-wq/VMPackerGO/internal/vm"
)

const maxFinalBytecodeSize = 256 * 1024

type AddrSpec struct {
	Addr uint64
	End  uint64
	Name string
}

func ParseAddrSpec(value string) (AddrSpec, error) {
	var spec AddrSpec
	if index := strings.LastIndex(value, ":"); index > 2 {
		candidate := value[index+1:]
		if _, err := strconv.ParseUint(candidate, 0, 64); err != nil {
			spec.Name = candidate
			value = value[:index]
		}
	}
	if parts := strings.Split(value, "-"); len(parts) == 2 {
		start, err := strconv.ParseUint(parts[0], 0, 64)
		if err != nil {
			return spec, fmt.Errorf("invalid start address: %s", parts[0])
		}
		end, err := strconv.ParseUint(parts[1], 0, 64)
		if err != nil {
			return spec, fmt.Errorf("invalid end address: %s", parts[1])
		}
		if end <= start {
			return spec, fmt.Errorf("end address must be greater than start address")
		}
		spec.Addr, spec.End = start, end
	} else {
		address, err := strconv.ParseUint(value, 0, 64)
		if err != nil {
			return spec, fmt.Errorf("invalid address: %s", value)
		}
		spec.Addr = address
	}
	if spec.Name == "" {
		spec.Name = fmt.Sprintf("sub_%X", spec.Addr)
	}
	return spec, nil
}

func PrintELFInfo(input []byte, path, mode string, out io.Writer) error {
	parsedMode := AndroidMode(strings.ToLower(mode))
	if parsedMode == "" {
		parsedMode = AndroidModeAuto
	}
	if _, err := parseELFMetadata(input, parsedMode); err != nil {
		return err
	}
	file, err := elf.NewFile(bytes.NewReader(input))
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintf(out, "ELF: %s\n", path)
	fmt.Fprintf(out, "  Arch: %s, Type: %s, Entry: 0x%X\n", file.Machine, file.Type, file.Entry)
	fmt.Fprintln(out, "\n  Sections:")
	for _, section := range file.Sections {
		if section.Size > 0 {
			fmt.Fprintf(out, "    %-16s  Addr=0x%08X  Size=0x%X  Off=0x%X\n",
				section.Name, section.Addr, section.Size, section.Offset)
		}
	}
	fmt.Fprintln(out, "\n  Program Headers:")
	for index, program := range file.Progs {
		flags := ""
		if program.Flags&elf.PF_R != 0 {
			flags += "R"
		}
		if program.Flags&elf.PF_W != 0 {
			flags += "W"
		}
		if program.Flags&elf.PF_X != 0 {
			flags += "X"
		}
		fmt.Fprintf(out, "    [%d] Type=%s Flags=%s Off=0x%X VA=0x%X FileSz=0x%X MemSz=0x%X\n",
			index, program.Type, flags, program.Off, program.Vaddr, program.Filesz, program.Memsz)
	}
	fmt.Fprintln(out, "\n  Functions:")
	symbols, err := file.Symbols()
	if err != nil {
		fmt.Fprintln(out, "  (no symbol table)")
		return nil
	}
	count := 0
	for _, symbol := range symbols {
		if elf.ST_TYPE(symbol.Info) == elf.STT_FUNC && symbol.Size > 0 {
			fmt.Fprintf(out, "    %-24s  Addr=0x%08X  Size=%d\n", symbol.Name, symbol.Value, symbol.Size)
			count++
		}
	}
	fmt.Fprintf(out, "  Total: %d functions\n", count)
	return nil
}

type Packer struct {
	opcodes vm.OpcodeMap
	verbose bool
	out     io.Writer
}

func (p *Packer) printf(format string, args ...any) {
	if p.verbose && p.out != nil {
		fmt.Fprintf(p.out, format, args...)
	}
}

func reverseInstructions(bytecode []byte, codeLen int, opcodes vm.OpcodeMap) ([]byte, map[int]int, error) {
	if codeLen < 0 || codeLen > len(bytecode) {
		return nil, nil, fmt.Errorf("invalid code length %d for %d-byte buffer", codeLen, len(bytecode))
	}
	type instruction struct{ offset, size int }
	var instructions []instruction
	for pc := 0; pc < codeLen; {
		wire := bytecode[pc]
		opcode, err := opcodes.Decode(wire)
		if err != nil {
			return nil, nil, fmt.Errorf("unknown VM wire opcode 0x%02X at offset 0x%X: %w", wire, pc, err)
		}
		size := vm.InstructionSize(opcode)
		if size == 0 {
			return nil, nil, fmt.Errorf("unknown VM opcode 0x%02X at offset 0x%X", opcode, pc)
		}
		if pc+size > codeLen {
			return nil, nil, fmt.Errorf("truncated VM instruction at offset 0x%X", pc)
		}
		instructions = append(instructions, instruction{offset: pc, size: size})
		pc += size
	}

	offsetMap := make(map[int]int, len(instructions))
	reversed := make([]byte, 0, codeLen+len(instructions))
	for index := len(instructions) - 1; index >= 0; index-- {
		instruction := instructions[index]
		newOffset := len(reversed)
		reversed = append(reversed, bytecode[instruction.offset:instruction.offset+instruction.size]...)
		reversed = append(reversed, byte(instruction.size))
		offsetMap[instruction.offset] = newOffset + instruction.size + 1
	}
	return reversed, offsetMap, nil
}

func (p *Packer) remapBranchTargets(bytecode []byte, codeLen int, offsetMap map[int]int) error {
	if codeLen < 0 || codeLen > len(bytecode) {
		return fmt.Errorf("invalid reversed code length %d", codeLen)
	}
	for pc := 0; pc < codeLen; {
		wire := bytecode[pc]
		opcode, err := p.opcodes.Decode(wire)
		if err != nil {
			return fmt.Errorf("unknown VM wire opcode 0x%02X at offset 0x%X: %w", wire, pc, err)
		}
		size := vm.InstructionSize(opcode)
		if size == 0 {
			return fmt.Errorf("unknown VM opcode 0x%02X at offset 0x%X", opcode, pc)
		}
		if pc+size >= codeLen || bytecode[pc+size] != byte(size) {
			return fmt.Errorf("invalid reversed instruction marker at offset 0x%X", pc)
		}
		if targetOffset := vm.BranchTargetOffset(opcode); targetOffset > 0 {
			if pc+targetOffset+4 > pc+size {
				return fmt.Errorf("truncated branch operand at offset 0x%X", pc)
			}
			oldTarget := binary.LittleEndian.Uint32(bytecode[pc+targetOffset:])
			newTarget, ok := offsetMap[int(oldTarget)]
			if !ok {
				return fmt.Errorf("branch at offset 0x%X references unknown target 0x%X", pc, oldTarget)
			}
			p.printf("      [REMAP] pc=0x%04X op=0x%02X target: 0x%04X -> 0x%04X\n", pc, wire, oldTarget, newTarget)
			binary.LittleEndian.PutUint32(bytecode[pc+targetOffset:], uint32(newTarget))
		}
		pc += size + 1
	}
	return nil
}

func encryptOpcodes(bytecode []byte, codeLen int, key uint32, reversed bool, opcodes vm.OpcodeMap) error {
	if codeLen < 0 || codeLen > len(bytecode) {
		return fmt.Errorf("invalid code length %d", codeLen)
	}
	for pc := 0; pc < codeLen; {
		wire := bytecode[pc]
		opcode, err := opcodes.Decode(wire)
		if err != nil {
			return fmt.Errorf("unknown VM wire opcode 0x%02X at offset 0x%X: %w", wire, pc, err)
		}
		size := vm.InstructionSize(opcode)
		if size == 0 {
			return fmt.Errorf("unknown VM opcode 0x%02X at offset 0x%X", opcode, pc)
		}
		step := size
		if reversed {
			step++
			if pc+size >= codeLen || bytecode[pc+size] != byte(size) {
				return fmt.Errorf("invalid reversed instruction marker at offset 0x%X", pc)
			}
		} else if pc+size > codeLen {
			return fmt.Errorf("truncated VM instruction at offset 0x%X", pc)
		}
		bytecode[pc] = wire ^ byte(key^(uint32(pc)*0x9E3779B9))
		pc += step
	}
	return nil
}

func validateFinalBytecodeSize(size int) error {
	if size < 0 || size > maxFinalBytecodeSize {
		return fmt.Errorf("generated %d bytes of final bytecode; maximum is %d", size, maxFinalBytecodeSize)
	}
	return nil
}

func validateBytecodeTrailer(bytecode []byte, codeLen int) (uint32, error) {
	if codeLen < 0 || codeLen > len(bytecode) || len(bytecode)-codeLen < 21 {
		return 0, fmt.Errorf("truncated trailer")
	}
	mapCount := binary.LittleEndian.Uint32(bytecode[len(bytecode)-16:])
	expected := uint64(mapCount)*8 + 21
	if expected != uint64(len(bytecode)-codeLen) {
		return 0, fmt.Errorf("map count %d does not match %d trailer bytes", mapCount, len(bytecode)-codeLen)
	}
	return mapCount, nil
}
