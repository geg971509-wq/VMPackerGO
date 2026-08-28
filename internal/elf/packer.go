package elf

import (
	"bytes"
	"crypto/rand"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/vmpacker/internal/arch/arm64"
	"github.com/vmpacker/internal/vm"
)

// ============================================================
// ELF 解析器 + 修改器 v3
//
// 注入策略: PT_NOTE 劫持或新增 PT_LOAD 段
//   1. 将 VM 解释器 blob + 加密字节码追加到文件末尾
//   2. 将 PT_NOTE 段转换为 PT_LOAD (RX)，或追加新的 PT_LOAD 段映射 payload
//   3. 新 LOAD 段使用独立的虚拟地址 (0x800000 起)
//   4. 原函数改写为跳板 → BL 到新段中的 VM 解释器
//
// 优点: 不移动任何现有数据，不破坏段对齐
// ============================================================

// AddrSpec 按地址指定函数
type AddrSpec struct {
	Addr uint64
	End  uint64 // 0 = 自动检测
	Name string // 可选名称
}

// ParseAddrSpec 解析地址规格: "0xADDR", "0xSTART-0xEND", "0xSTART-0xEND:name"
func ParseAddrSpec(s string) (AddrSpec, error) {
	var spec AddrSpec
	// 分离可选名称 (最后一个冒号后面)
	if idx := strings.LastIndex(s, ":"); idx > 2 {
		candidate := s[idx+1:]
		// 如果不像十六进制数则是名称
		if _, err := strconv.ParseUint(candidate, 0, 64); err != nil {
			spec.Name = candidate
			s = s[:idx]
		}
	}
	// 解析地址范围
	if parts := strings.Split(s, "-"); len(parts) == 2 {
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
		spec.Addr = start
		spec.End = end
	} else {
		addr, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			return spec, fmt.Errorf("invalid address: %s", s)
		}
		spec.Addr = addr
	}
	if spec.Name == "" {
		spec.Name = fmt.Sprintf("sub_%X", spec.Addr)
	}
	return spec, nil
}

// Packer ELF VMP packer.
type Packer struct {
	selections   []Selection
	analysis     Analysis
	verbose      bool
	stripSymbols bool
	debug        bool
	targetOS     string
	androidMode  AndroidMode
	injector     InjectorKind

	selectedInjector InjectorKind
	injectorReason   string
	targetMeta       *elfTargetMetadata
	result           Result
	debugLog         bytes.Buffer
	out              io.Writer

	data       []byte
	interpBlob []byte
	opcodes    vm.OpcodeMap
}

// FuncBytecode 保存单个函数的加密字节码和元信息
type FuncBytecode struct {
	FI        *vm.FuncInfo
	Encrypted []byte
	XorKey    byte
}

type runtimeBlob struct {
	entryOff        uint64
	tokenEntryOff   uint64
	tokenTableVAOff uint64
	code            []byte
}

func parseRuntimeBlob(blob []byte) (runtimeBlob, error) {
	if len(blob) < 24 {
		return runtimeBlob{}, fmt.Errorf("token mode requires extended blob header (24 bytes), got %d", len(blob))
	}
	parsed := runtimeBlob{
		entryOff:        binary.LittleEndian.Uint64(blob[:8]),
		tokenEntryOff:   binary.LittleEndian.Uint64(blob[8:16]),
		tokenTableVAOff: binary.LittleEndian.Uint64(blob[16:24]),
		code:            blob[24:],
	}
	if parsed.tokenEntryOff == 0 {
		return runtimeBlob{}, fmt.Errorf("vm_entry_token not found in blob (compile with -DVM_TOKEN_ENTRY)")
	}
	if parsed.tokenTableVAOff == 0 {
		return runtimeBlob{}, fmt.Errorf("_token_table_va not found in blob (compile with -DVM_TOKEN_ENTRY)")
	}
	for _, field := range []struct {
		name   string
		offset uint64
		width  uint64
	}{
		{name: "vm_entry", offset: parsed.entryOff, width: 4},
		{name: "vm_entry_token", offset: parsed.tokenEntryOff, width: 4},
		{name: "_token_table_va", offset: parsed.tokenTableVAOff, width: 8},
	} {
		end, ok := checkedAdd(field.offset, field.width)
		if !ok || end > uint64(len(parsed.code)) {
			return runtimeBlob{}, fmt.Errorf("%s offset 0x%X with %d-byte access exceeds %d-byte interpreter code", field.name, field.offset, field.width, len(parsed.code))
		}
	}
	return parsed, nil
}

func (p *Packer) extractSelectionCode(selection Selection) ([]byte, error) {
	end, ok := checkedAdd(selection.Offset, selection.Size())
	if !ok || end > uint64(len(p.data)) {
		return nil, fmt.Errorf("validated function %q exceeds input bounds", selection.Name)
	}
	return append([]byte(nil), p.data[selection.Offset:end]...), nil
}

// DecodeFunction 解码 ARM64 指令
func (p *Packer) DecodeFunction(code []byte) []vm.Instruction {
	dec := arm64.NewDecoder()
	var insts []vm.Instruction
	for off := 0; off+4 <= len(code); off += 4 {
		raw := binary.LittleEndian.Uint32(code[off:])
		inst := dec.Decode(raw, off)
		insts = append(insts, inst)
	}
	return insts
}

// processBytes transforms one analyzed Android ELF without writing files.
func (p *Packer) processBytes(input []byte) error {
	p.result.Functions = []FunctionFact{}
	p.result.TargetKind = p.analysis.TargetKind
	p.result.AnalysisLimitations = append([]string(nil), p.analysis.Limitations...)
	p.result.Warnings = append([]string(nil), p.analysis.Warnings...)
	runtime, err := parseRuntimeBlob(p.interpBlob)
	if err != nil {
		return err
	}
	p.data = append([]byte(nil), input...)

	meta := &elfTargetMetadata{Kind: p.analysis.TargetKind, HasNote: p.analysis.hasNote}
	if err := p.selectInjector(meta); err != nil {
		return err
	}
	p.result.DevelopmentStrategy = string(p.selectedInjector)
	p.printf("[*] ELF: EM_AARCH64, Target: %s, Kind: %s\n", p.targetOS, p.analysis.TargetKind)
	p.printf("[*] Injector: %s (%s)\n", p.selectedInjector, p.injectorReason)
	p.printf("[*] VM interp blob: %d bytes\n", len(p.interpBlob))

	dec := arm64.NewDecoder()
	var funcs []FuncBytecode
	p.opcodes = vm.IdentityOpcodeMap()
	for _, selection := range p.selections {
		p.printf("\n[*] Processing: %s\n", selection.Name)
		fi := &vm.FuncInfo{Name: selection.Name, Addr: selection.Address, Size: selection.Size(), Offset: selection.Offset, Section: selection.Section}
		p.printf("    Addr: 0x%X, Size: %d bytes, Section: %s\n", fi.Addr, fi.Size, fi.Section)

		code, err := p.extractSelectionCode(selection)
		if err != nil {
			return err
		}

		insts := p.DecodeFunction(code)
		if len(insts) > maxInferredInstructions {
			return fmt.Errorf("function %q has %d instructions; maximum is %d", selection.Name, len(insts), maxInferredInstructions)
		}
		p.printf("    Instructions: %d\n", len(insts))

		if p.verbose {
			p.println("    --- Disasm ---")
			for _, inst := range insts {
				p.printf("    0x%04X: %-12s raw=0x%08X\n",
					inst.Offset, dec.InstName(inst.Op), inst.Raw)
			}
			p.println("    --- End ---")
		}

		trans, err := arm64.NewTranslator(fi.Addr, int(fi.Size), p.opcodes)
		if err != nil {
			return fmt.Errorf("create translator for %s: %w", selection.Name, err)
		}
		if p.debug {
			trans.SetDebug(true)
		}
		result, err := trans.Translate(insts)
		if err != nil {
			return fmt.Errorf("translation failed: %v", err)
		}

		p.printf("    Translated: %d/%d\n", result.TransInsts, result.TotalInsts)
		p.printf("    Bytecode: %d bytes\n", len(result.Bytecode))
		p.result.Functions = append(p.result.Functions, FunctionFact{
			Source: selection.Source, Selector: selection.Selector, Name: fi.Name,
			Address: fi.Addr, End: selection.End, Size: fi.Size, Section: fi.Section, SymbolSource: selection.SymbolSource,
			Bytecode: len(result.Bytecode), Translated: result.TransInsts, Instructions: result.TotalInsts,
		})

		if len(result.Unsupported) > 0 {
			p.printf("    [!] Unsupported (%d):\n", len(result.Unsupported))
			for _, unsupported := range result.Unsupported {
				p.printf("        %s\n", unsupported)
			}
			if p.debug {
				fmt.Fprintf(&p.debugLog, "Translation failed: %s @ 0x%X\n", selection.Name, fi.Addr)
				fmt.Fprintf(&p.debugLog, "Function size: %d bytes; instructions: %d; translated: %d\n",
					fi.Size, result.TotalInsts, result.TransInsts)
				for i, unsupported := range result.Unsupported {
					fmt.Fprintf(&p.debugLog, "[%d] %s\n", i+1, unsupported)
				}
			}
			return fmt.Errorf("translation aborted: %d unsupported instruction(s) in %s; no artifact was produced",
				len(result.Unsupported), selection.Name)
		}

		// Capture the mapping before bytecode reversal/encryption.
		if p.debug {
			fmt.Fprintf(&p.debugLog, "Function: %s @ 0x%X (size: %d)\n", selection.Name, fi.Addr, fi.Size)
			fmt.Fprintf(&p.debugLog, "VM bytecode: %d bytes (pre-reverse)\n\n", len(result.Bytecode))
			for _, dbg := range trans.DebugLog() {
				fmt.Fprintf(&p.debugLog, "ARM64  %04X: %-16s  (raw=0x%08X)\n",
					dbg.ARM64Offset, dbg.ARM64Asm, dbg.ARM64Raw)
				lines, err := vm.DisasmRange(result.Bytecode, dbg.VMStart, dbg.VMEnd, p.opcodes)
				if err != nil {
					return fmt.Errorf("disassemble %s: %w", selection.Name, err)
				}
				for _, line := range lines {
					fmt.Fprintf(&p.debugLog, "  VM   %s\n", line)
				}
				fmt.Fprintln(&p.debugLog)
			}
		}

		// ---- PC 反向遍历: 反转指令顺序 ----
		// 必须在 OpcodeCryptor 之前执行 (加密使用最终 pc 位置)
		reversed, offsetMap, err := reverseInstructions(result.Bytecode, result.CodeLen, p.opcodes)
		if err != nil {
			return fmt.Errorf("reverse bytecode for %s: %w", selection.Name, err)
		}

		// 重映射分支目标 (使用反转后的偏移)
		newCodeLen := len(reversed)
		if err := p.remapBranchTargets(reversed, newCodeLen, offsetMap); err != nil {
			return fmt.Errorf("remap bytecode for %s: %w", selection.Name, err)
		}

		// 重映射 addr_map 中的 vm_off (BR 间接跳转)
		// trailer 在 result.Bytecode[result.CodeLen:] 中，每个 entry 8B: [arm64_off:u32][vm_off:u32]
		mapCount, err := validateBytecodeTrailer(result.Bytecode, result.CodeLen)
		if err != nil {
			return fmt.Errorf("bytecode trailer for %s: %w", selection.Name, err)
		}
		trailerStart := result.CodeLen
		for j := 0; j < int(mapCount); j++ {
			entryOff := trailerStart + j*8
			vmOff := binary.LittleEndian.Uint32(result.Bytecode[entryOff+4:])
			newVmOff, ok := offsetMap[int(vmOff)]
			if !ok {
				return fmt.Errorf("bytecode trailer for %s references unknown VM offset 0x%X", selection.Name, vmOff)
			}
			binary.LittleEndian.PutUint32(result.Bytecode[entryOff+4:], uint32(newVmOff))
		}

		// 用反转后的字节码替换原始指令区，保留 trailer
		trailer := result.Bytecode[result.CodeLen:]
		finalBytecode := make([]byte, 0, newCodeLen+len(trailer))
		finalBytecode = append(finalBytecode, reversed...)
		finalBytecode = append(finalBytecode, trailer...)
		result.Bytecode = finalBytecode
		result.CodeLen = newCodeLen
		if err := validateFinalBytecodeSize(len(result.Bytecode)); err != nil {
			return fmt.Errorf("function %q: %w", selection.Name, err)
		}
		p.result.Functions[len(p.result.Functions)-1].Bytecode = len(result.Bytecode)

		if p.verbose {
			p.printf("    [REV] reversed: %d insts, newCodeLen=%d (was %d), offsetMap entries=%d\n",
				len(offsetMap), newCodeLen, result.CodeLen, len(offsetMap))
		}

		// ---- OpcodeCryptor: 逐指令 opcode 加密 ----
		// 生成随机 oc_key (4 字节)
		var ocKeyBuf [4]byte
		if _, err := rand.Read(ocKeyBuf[:]); err != nil {
			return fmt.Errorf("generating oc_key failed: %v", err)
		}
		ocKey := binary.LittleEndian.Uint32(ocKeyBuf[:])

		// 加密字节码中每条指令的 opcode 字节 (仅 [0:CodeLen] 范围)
		// reversed=true: 每条指令后有 1B size 标记
		if err := encryptOpcodes(result.Bytecode, result.CodeLen, ocKey, true, p.opcodes); err != nil {
			return fmt.Errorf("encrypt bytecode for %s: %w", selection.Name, err)
		}

		// 将 reverse 标志 + oc_key 写入 trailer 占位位置
		// trailer: [BR map entries][reverse(1B)][oc_key(4B)][map_count][func_addr][func_size]
		// reverse 位于 BR map 之后
		reverseOffset := result.CodeLen + int(mapCount)*8 // BR map 之后
		result.Bytecode[reverseOffset] = 1                // reverse = 1
		ocKeyOffset := reverseOffset + 1                  // reverse(1B) 之后
		binary.LittleEndian.PutUint32(result.Bytecode[ocKeyOffset:], ocKey)

		if p.verbose {
			p.printf("    [OC] oc_key=0x%08X, codeLen=%d, mapCount=%d, reverseOff=%d, keyOff=%d\n",
				ocKey, result.CodeLen, mapCount, reverseOffset, ocKeyOffset)
		}

		// ---- XOR chain 加密 (整段字节码) ----
		xorKey := byte(0xA5)
		encrypted := make([]byte, len(result.Bytecode))
		for i, b := range result.Bytecode {
			encrypted[i] = b ^ xorKey
		}

		funcs = append(funcs, FuncBytecode{FI: fi, Encrypted: encrypted, XorKey: xorKey})
	}

	// 第二阶段: 批量注入 (一次 PT_NOTE 劫持)
	p.printf("\n[*] Injecting %d functions...\n", len(funcs))
	err = p.injectVMPBatch(runtime, funcs)
	if err != nil {
		return fmt.Errorf("injection failed: %v", err)
	}

	for _, fb := range funcs {
		p.printf("    [+] %s VMP protected\n", fb.FI.Name)
	}

	// 第三阶段: 清除符号表 (可选)
	if p.stripSymbols {
		p.stripSections()
		p.println("[*] Symbols stripped")
	}

	p.result.Artifact = append([]byte(nil), p.data...)
	return nil
}

func (p *Packer) printf(format string, args ...any) {
	fmt.Fprintf(p.out, format, args...)
}

func (p *Packer) println(args ...any) {
	fmt.Fprintln(p.out, args...)
}

// stripSections 就地清除符号/调试 section
// stripSections 清除符号表等 section（等效 strip -s）
// 不改变文件布局和 section header 数量，只将目标 section 置空
// 同时修复其他 section 对被删除 section 的 sh_link 引用
func (p *Packer) stripSections() {
	ehdr := readEhdr64(p.data)
	if ehdr.Shoff == 0 || ehdr.Shnum == 0 {
		return
	}

	// 读取 section name string table
	shstrIdx := binary.LittleEndian.Uint16(p.data[0x3E:])
	shstrOff := ehdr.Shoff + uint64(shstrIdx)*uint64(ehdr.Shentsize)
	shstrSecOff := binary.LittleEndian.Uint64(p.data[shstrOff+24:])
	shstrSecSz := binary.LittleEndian.Uint64(p.data[shstrOff+32:])

	getSectionName := func(nameOff uint32) string {
		start := shstrSecOff + uint64(nameOff)
		if start >= uint64(len(p.data)) {
			return ""
		}
		end := start
		for end < shstrSecOff+shstrSecSz && end < uint64(len(p.data)) && p.data[end] != 0 {
			end++
		}
		return string(p.data[start:end])
	}

	// 要清除的 section 名称
	stripNames := map[string]bool{
		".symtab":            true,
		".strtab":            true,
		".comment":           true,
		".note.GNU-stack":    true,
		".note.gnu.build-id": true,
	}

	// 第一遍: 收集被删除的 section index
	stripped := make(map[int]bool)
	for i := 0; i < int(ehdr.Shnum); i++ {
		shOff := ehdr.Shoff + uint64(i)*uint64(ehdr.Shentsize)
		nameOff := binary.LittleEndian.Uint32(p.data[shOff:])
		name := getSectionName(nameOff)
		if stripNames[name] {
			stripped[i] = true
		}
	}

	// 第二遍: 清零被删除的 section，修复 sh_link 引用
	for i := 0; i < int(ehdr.Shnum); i++ {
		shOff := ehdr.Shoff + uint64(i)*uint64(ehdr.Shentsize)

		if stripped[i] {
			// 读取 section 的文件偏移和大小
			secOff := binary.LittleEndian.Uint64(p.data[shOff+24:])
			secSz := binary.LittleEndian.Uint64(p.data[shOff+32:])

			// 用 0x00 清零 section 内容（等效 strip -s）
			if secOff+secSz <= uint64(len(p.data)) {
				for j := uint64(0); j < secSz; j++ {
					p.data[secOff+j] = 0
				}
			}

			nameOff := binary.LittleEndian.Uint32(p.data[shOff:])
			name := getSectionName(nameOff)

			// 清零整个 section header entry（保留 sh_name 用于调试）
			// sh_type = SHT_NULL
			binary.LittleEndian.PutUint32(p.data[shOff+4:], 0)
			// sh_flags = 0
			binary.LittleEndian.PutUint64(p.data[shOff+8:], 0)
			// sh_addr = 0
			binary.LittleEndian.PutUint64(p.data[shOff+16:], 0)
			// sh_offset = 0
			binary.LittleEndian.PutUint64(p.data[shOff+24:], 0)
			// sh_size = 0
			binary.LittleEndian.PutUint64(p.data[shOff+32:], 0)
			// sh_link = 0
			binary.LittleEndian.PutUint32(p.data[shOff+40:], 0)
			// sh_info = 0
			binary.LittleEndian.PutUint32(p.data[shOff+44:], 0)
			// sh_addralign = 0
			binary.LittleEndian.PutUint64(p.data[shOff+48:], 0)
			// sh_entsize = 0
			binary.LittleEndian.PutUint64(p.data[shOff+56:], 0)

			if p.verbose {
				p.printf("    [strip] %s cleared (off=0x%X, sz=%d)\n", name, secOff, secSz)
			}
		} else {
			// 非被删除的 section: 检查 sh_link 是否指向被删除的 section
			shLink := binary.LittleEndian.Uint32(p.data[shOff+40:])
			if shLink > 0 && stripped[int(shLink)] {
				binary.LittleEndian.PutUint32(p.data[shOff+40:], 0) // 清零 sh_link
				if p.verbose {
					nameOff := binary.LittleEndian.Uint32(p.data[shOff:])
					name := getSectionName(nameOff)
					p.printf("    [strip] %s: sh_link %d → 0 (target stripped)\n", name, shLink)
				}
			}
		}
	}
}

// injectVMPBatch — 批量注入 VM payload 并写入 Token 跳板
func (p *Packer) injectVMPBatch(runtime runtimeBlob, funcs []FuncBytecode) error {
	ehdr := readEhdr64(p.data)
	entryOff, tokenEntryOff, tokenTableVAOff := runtime.entryOff, runtime.tokenEntryOff, runtime.tokenTableVAOff
	interpCode := runtime.code

	// 1. 构造 payload: [interpCode][bc0][pad][bc1][pad][...]
	payload := make([]byte, 0, len(interpCode)+1024)
	payload = append(payload, interpCode...)
	for len(payload)%4 != 0 {
		payload = append(payload, 0x00)
	}

	type bcRecord struct {
		payloadOff int
		bcLen      int
	}
	records := make([]bcRecord, len(funcs))

	for i, fb := range funcs {
		records[i].payloadOff = len(payload)
		records[i].bcLen = len(fb.Encrypted)
		payload = append(payload, fb.Encrypted...)
		for len(payload)%4 != 0 {
			payload = append(payload, 0x00)
		}
	}

	// 2. 追加到文件末尾 (页对齐，兼容 QEMU 用户态)
	// 先将文件填充到页边界
	appendOff := uint64(len(p.data))
	padLen := (0x1000 - (appendOff % 0x1000)) % 0x1000
	for i := uint64(0); i < padLen; i++ {
		p.data = append(p.data, 0x00)
	}
	payloadFileOff := uint64(len(p.data)) // 现在是页对齐的
	// 动态计算 payloadVA: 扫描所有 LOAD 段，取最高 Vaddr+Memsz，向上对齐到 64KB
	var maxVA uint64
	for i := 0; i < int(ehdr.Phnum); i++ {
		phOff := ehdr.Phoff + uint64(i)*uint64(ehdr.Phentsize)
		ph := readPhdr64(p.data, phOff)
		if ph.Type == uint32(elf.PT_LOAD) {
			end := ph.Vaddr + ph.Memsz
			if end > maxVA {
				maxVA = end
			}
		}
	}
	payloadVA := (maxVA + 0xFFFF) &^ 0xFFFF // 向上对齐到 64KB 边界

	p.data = append(p.data, payload...)

	interpVA := payloadVA + entryOff // vm_entry 偏移由 Makefile 自动注入到 blob 头部

	p.printf("    Payload at file offset: 0x%X, VA: 0x%X, size: %d\n",
		payloadFileOff, payloadVA, len(payload))
	p.printf("    VM interp VA: 0x%X\n", interpVA)

	for i, fb := range funcs {
		bcVA := payloadVA + uint64(records[i].payloadOff)
		p.printf("    [%s] bytecode VA: 0x%X, len: %d\n",
			fb.FI.Name, bcVA, records[i].bcLen)
	}

	// 3. 分配 payload PT_LOAD 的 program-header 槽位。
	payloadSlot, err := p.allocatePayloadSegmentSlot(ehdr)
	if err != nil {
		return err
	}
	ehdr = payloadSlot.ehdr

	// 4. 写入 payload PT_LOAD (RX)
	payloadPhdrOff := payloadSlot.off
	newPhdr := elf64Phdr{
		Type:   uint32(elf.PT_LOAD),
		Flags:  uint32(elf.PF_R | elf.PF_X),
		Off:    payloadFileOff,
		Vaddr:  payloadVA,
		Paddr:  payloadVA,
		Filesz: uint64(len(payload)),
		Memsz:  uint64(len(payload)),
		Align:  0x1000,
	}
	writePhdr64(p.data, payloadPhdrOff, newPhdr)

	if p.selectedInjector == InjectorNoteHijack {
		p.printf("    PT_NOTE[%d] -> PT_LOAD RX: off=0x%X va=0x%X sz=0x%X\n",
			payloadSlot.index, payloadFileOff, payloadVA, len(payload))
	} else {
		p.printf("    AddSegment[%d:%s] -> PT_LOAD RX: off=0x%X va=0x%X sz=0x%X\n",
			payloadSlot.index, payloadSlot.source, payloadFileOff, payloadVA, len(payload))
	}
	phdrIndex := payloadSlot.index
	p.result.Injection = &InjectionFact{
		Strategy: p.selectedInjector, PhdrIndex: &phdrIndex, SegmentSource: payloadSlot.source,
		PayloadOffset: payloadFileOff, PayloadVA: payloadVA, PayloadSize: uint64(len(payload)), VMEntryVA: interpVA,
	}

	// 4b. 按 Vaddr 升序重排所有 PT_LOAD 段，防止内核映射 BSS 失败
	{
		type phdrSlot struct {
			idx  int
			phdr elf64Phdr
		}
		var loads []phdrSlot
		for i := 0; i < int(ehdr.Phnum); i++ {
			off := ehdr.Phoff + uint64(i)*uint64(ehdr.Phentsize)
			ph := readPhdr64(p.data, off)
			if ph.Type == uint32(elf.PT_LOAD) {
				loads = append(loads, phdrSlot{idx: i, phdr: ph})
			}
		}
		// 检查是否需要重排
		needSort := false
		for k := 1; k < len(loads); k++ {
			if loads[k].phdr.Vaddr < loads[k-1].phdr.Vaddr {
				needSort = true
				break
			}
		}
		if needSort {
			// 按 Vaddr 排序 PHDR 内容
			sort.Slice(loads, func(a, b int) bool {
				return loads[a].phdr.Vaddr < loads[b].phdr.Vaddr
			})
			// 收集原始 PHDR 槽位索引（按在 PHDR 表中出现的顺序）
			slotIndices := make([]int, len(loads))
			for k := range loads {
				slotIndices[k] = loads[k].idx
			}
			sort.Ints(slotIndices)
			// 将排序后的 PHDR 内容写回原始槽位
			for k, si := range slotIndices {
				off := ehdr.Phoff + uint64(si)*uint64(ehdr.Phentsize)
				writePhdr64(p.data, off, loads[k].phdr)
			}
			p.printf("    [PHDR] Reordered %d PT_LOAD segments by Vaddr ascending\n", len(loads))
			// 更新 notePhdrOff — 找到 payload 段的新位置
			for i := 0; i < int(ehdr.Phnum); i++ {
				off := ehdr.Phoff + uint64(i)*uint64(ehdr.Phentsize)
				ph := readPhdr64(p.data, off)
				if ph.Type == uint32(elf.PT_LOAD) && ph.Vaddr == payloadVA {
					payloadPhdrOff = off
					break
				}
			}
		}
	}

	// 5a. 构建 token_desc_t 描述符表
	// 8-byte 对齐
	for len(payload)%8 != 0 {
		payload = append(payload, 0x00)
	}
	tokenTableOff := len(payload)
	tokenTableVA := payloadVA + uint64(tokenTableOff)

	// 每个函数一个 token_desc_t (16 bytes): bc_off(u64) + bc_len(u32) + reserved(u32)
	// bc_off = 相对于 _token_table_va 自身地址的偏移 (PIE 兼容)
	selfVA := payloadVA + tokenTableVAOff // _token_table_va 的 VA
	for i := range funcs {
		bcVA := payloadVA + uint64(records[i].payloadOff)
		bcLen := uint32(records[i].bcLen)

		var desc [16]byte
		binary.LittleEndian.PutUint64(desc[0:], bcVA-selfVA) // 相对偏移
		binary.LittleEndian.PutUint32(desc[8:], bcLen)
		binary.LittleEndian.PutUint32(desc[12:], 0) // reserved
		payload = append(payload, desc[:]...)
	}

	// 更新 PT_LOAD 段大小 (payload 增长了)
	newPhdr.Filesz = uint64(len(payload))
	newPhdr.Memsz = uint64(len(payload))
	writePhdr64(p.data, payloadPhdrOff, newPhdr)

	// 重新追加 payload 到文件 (覆盖之前的)
	p.data = p.data[:payloadFileOff]
	p.data = append(p.data, payload...)

	// 5b. Patch _token_table_va: 存储相对于自身地址的偏移 (PIE 兼容)
	// selfVA = payloadVA + tokenTableVAOff (已在上面计算)
	tblRelOff := tokenTableVA - selfVA
	binary.LittleEndian.PutUint64(p.data[payloadFileOff+tokenTableVAOff:], tblRelOff)

	p.printf("    [TOKEN] descriptor table VA: 0x%X, entries: %d\n", tokenTableVA, len(funcs))
	p.printf("    [TOKEN] _token_table_va patched at blob offset 0x%X → relative offset 0x%X (PIE)\n", tokenTableVAOff, tblRelOff)

	// 5c. 为每个函数生成 Token trampoline
	vmEntryTokenVA := payloadVA + tokenEntryOff
	p.printf("    [TOKEN] vm_entry_token VA: 0x%X\n", vmEntryTokenVA)
	p.result.Injection.PayloadSize = uint64(len(payload))
	p.result.Injection.TokenEntryVA = vmEntryTokenVA

	for i, fb := range funcs {
		funcID := uint32(i) // func_id = 序号 (0-based)
		token := (uint32(fb.XorKey) << 24) | (0 << 12) | (funcID & 0xFFF)

		trampoline, err := BuildTokenTrampoline(fb.FI.Addr, vmEntryTokenVA, token)
		if err != nil {
			return fmt.Errorf("token trampoline for %s cannot reach VM entry: %v", fb.FI.Name, err)
		}
		if uint64(len(trampoline)) > fb.FI.Size {
			return fmt.Errorf("token trampoline for %s (%d bytes) exceeds function size (%d bytes)",
				fb.FI.Name, len(trampoline), fb.FI.Size)
		}

		// 写入跳板
		for j := 0; j < len(trampoline); j++ {
			p.data[fb.FI.Offset+uint64(j)] = trampoline[j]
		}

		// 销毁剩余原始代码
		garbageLen := int(fb.FI.Size) - len(trampoline)
		if garbageLen > 0 {
			garbage := make([]byte, garbageLen)
			if _, err := rand.Read(garbage); err != nil {
				return fmt.Errorf("generate replacement bytes for %s: %w", fb.FI.Name, err)
			}
			copy(p.data[fb.FI.Offset+uint64(len(trampoline)):], garbage)
		}

		p.printf("    [TOKEN] %s: func_id=%d, token=0x%08X, trampoline=%d bytes\n",
			fb.FI.Name, funcID, token, len(trampoline))
	}

	return nil
}

// PrintELFInfo prints information from already bounded input bytes.
func PrintELFInfo(input []byte, path, mode string, out io.Writer) error {
	f, err := elf.NewFile(bytes.NewReader(input))
	if err != nil {
		return err
	}
	defer f.Close()
	if f.Machine != elf.EM_AARCH64 || f.Class != elf.ELFCLASS64 {
		return fmt.Errorf("input must be an Android AArch64 ELF64 file")
	}
	if _, err := classifyELFTarget("android", AndroidMode(mode), f); err != nil {
		return err
	}

	fmt.Fprintf(out, "ELF: %s\n", path)
	fmt.Fprintf(out, "  Arch: %s, Type: %s, Entry: 0x%X\n", f.Machine, f.Type, f.Entry)
	fmt.Fprintln(out, "\n  Sections:")
	for _, section := range f.Sections {
		if section.Size > 0 {
			fmt.Fprintf(out, "    %-16s  Addr=0x%08X  Size=0x%X  Off=0x%X\n",
				section.Name, section.Addr, section.Size, section.Offset)
		}
	}
	fmt.Fprintln(out, "\n  Program Headers:")
	for i, prog := range f.Progs {
		flags := ""
		if prog.Flags&elf.PF_R != 0 {
			flags += "R"
		}
		if prog.Flags&elf.PF_W != 0 {
			flags += "W"
		}
		if prog.Flags&elf.PF_X != 0 {
			flags += "X"
		}
		fmt.Fprintf(out, "    [%d] Type=%s Flags=%s Off=0x%X VA=0x%X FileSz=0x%X MemSz=0x%X\n",
			i, prog.Type, flags, prog.Off, prog.Vaddr, prog.Filesz, prog.Memsz)
	}
	fmt.Fprintln(out, "\n  Functions:")
	syms, err := f.Symbols()
	if err != nil {
		fmt.Fprintln(out, "  (no symbol table)")
		return nil
	}
	count := 0
	for _, sym := range syms {
		if elf.ST_TYPE(sym.Info) == elf.STT_FUNC && sym.Size > 0 {
			fmt.Fprintf(out, "    %-24s  Addr=0x%08X  Size=%d\n", sym.Name, sym.Value, sym.Size)
			count++
		}
	}
	fmt.Fprintf(out, "  Total: %d functions\n", count)
	return nil
}

// reverseInstructions 反转指令顺序并追加 size 标记
//
// 输入: bytecode[0:codeLen] 为纯指令区 (不含 trailer)
// 输出: 反转后的字节码 + old_offset→new_offset 映射
//
// 反转后每条指令后追加 1 字节 size 标记:
//
//	[inst_N bytes][size_N(1B)][inst_N-1 bytes][size_N-1(1B)]...
//
// stub 解释器反向遍历: pc--; size=bc[pc]; pc-=size; → 定位到指令起始
func reverseInstructions(bytecode []byte, codeLen int, opcodes vm.OpcodeMap) ([]byte, map[int]int, error) {
	if codeLen < 0 || codeLen > len(bytecode) {
		return nil, nil, fmt.Errorf("invalid code length %d for %d-byte buffer", codeLen, len(bytecode))
	}
	type instInfo struct {
		offset int
		size   int
	}
	var insts []instInfo
	for pc := 0; pc < codeLen; {
		wire := bytecode[pc]
		op, err := opcodes.Decode(wire)
		if err != nil {
			return nil, nil, fmt.Errorf("unknown VM wire opcode 0x%02X at offset 0x%X: %w", wire, pc, err)
		}
		sz := vm.InstructionSize(op)
		if sz == 0 {
			return nil, nil, fmt.Errorf("unknown VM opcode 0x%02X at offset 0x%X", op, pc)
		}
		if pc+sz > codeLen {
			return nil, nil, fmt.Errorf("truncated VM instruction at offset 0x%X", pc)
		}
		insts = append(insts, instInfo{offset: pc, size: sz})
		pc += sz
	}

	offsetMap := make(map[int]int, len(insts))
	reversed := make([]byte, 0, codeLen+len(insts))
	for i := len(insts) - 1; i >= 0; i-- {
		inst := insts[i]
		newOffset := len(reversed)
		reversed = append(reversed, bytecode[inst.offset:inst.offset+inst.size]...)
		reversed = append(reversed, byte(inst.size))
		offsetMap[inst.offset] = newOffset + inst.size + 1
	}
	return reversed, offsetMap, nil
}

// remapBranchTargets 重映射反转后字节码中的分支目标
//
// 扫描 reversed bytecode，找到所有分支指令，
// 将其 target32 从旧偏移替换为新偏移 (使用 offsetMap)
func (p *Packer) remapBranchTargets(bytecode []byte, codeLen int, offsetMap map[int]int) error {
	if codeLen < 0 || codeLen > len(bytecode) {
		return fmt.Errorf("invalid reversed code length %d", codeLen)
	}
	for pc := 0; pc < codeLen; {
		wire := bytecode[pc]
		op, err := p.opcodes.Decode(wire)
		if err != nil {
			return fmt.Errorf("unknown VM wire opcode 0x%02X at offset 0x%X: %w", wire, pc, err)
		}
		sz := vm.InstructionSize(op)
		if sz == 0 {
			return fmt.Errorf("unknown VM opcode 0x%02X at offset 0x%X", op, pc)
		}
		if pc+sz >= codeLen || bytecode[pc+sz] != byte(sz) {
			return fmt.Errorf("invalid reversed instruction marker at offset 0x%X", pc)
		}
		if toff := vm.BranchTargetOffset(op); toff > 0 {
			if pc+toff+4 > pc+sz {
				return fmt.Errorf("truncated branch operand at offset 0x%X", pc)
			}
			oldTarget := binary.LittleEndian.Uint32(bytecode[pc+toff:])
			newTarget, ok := offsetMap[int(oldTarget)]
			if !ok {
				return fmt.Errorf("branch at offset 0x%X references unknown target 0x%X", pc, oldTarget)
			}
			if p.verbose {
				p.printf("      [REMAP] pc=0x%04X op=0x%02X target: 0x%04X → 0x%04X\n", pc, wire, oldTarget, newTarget)
			}
			binary.LittleEndian.PutUint32(bytecode[pc+toff:], uint32(newTarget))
		}
		pc += sz + 1
	}
	return nil
}

// encryptOpcodes 逐指令加密 opcode 字节 (OpcodeCryptor)
//
// 遍历 bytecode[0:codeLen]，使用 vm.InstructionSize 确定每条指令的大小，
// 只加密每条指令的第一个字节 (opcode)，操作数不变。
//
// reversed=true 时，每条指令后有 1B size 标记，步进为 size+1
//
// 加密公式: encrypted_opcode[pc] = opcode[pc] ^ (u8)(ocKey ^ (pc * 0x9E3779B9))
func encryptOpcodes(bytecode []byte, codeLen int, ocKey uint32, reversed bool, opcodes vm.OpcodeMap) error {
	if codeLen < 0 || codeLen > len(bytecode) {
		return fmt.Errorf("invalid code length %d", codeLen)
	}
	for pc := 0; pc < codeLen; {
		wire := bytecode[pc]
		op, err := opcodes.Decode(wire)
		if err != nil {
			return fmt.Errorf("unknown VM wire opcode 0x%02X at offset 0x%X: %w", wire, pc, err)
		}
		size := vm.InstructionSize(op)
		if size == 0 {
			return fmt.Errorf("unknown VM opcode 0x%02X at offset 0x%X", op, pc)
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
		mask := byte(ocKey ^ (uint32(pc) * 0x9E3779B9))
		bytecode[pc] = wire ^ mask
		pc += step
	}
	return nil
}

func validateFinalBytecodeSize(size int) error {
	if size > 64*1024 {
		return fmt.Errorf("generated %d bytes of final bytecode; maximum is 65536", size)
	}
	return nil
}

func validateBytecodeTrailer(bytecode []byte, codeLen int) (uint32, error) {
	if codeLen < 0 || codeLen > len(bytecode) || len(bytecode)-codeLen < 21 {
		return 0, fmt.Errorf("truncated trailer")
	}
	mapCount := binary.LittleEndian.Uint32(bytecode[len(bytecode)-16:])
	mapBytes := uint64(mapCount) * 8
	expected := mapBytes + 21
	if expected != uint64(len(bytecode)-codeLen) {
		return 0, fmt.Errorf("map count %d does not match %d trailer bytes", mapCount, len(bytecode)-codeLen)
	}
	return mapCount, nil
}
