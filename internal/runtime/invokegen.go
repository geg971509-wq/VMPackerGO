package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vmpacker/internal/unwind"
)

var exceptionInvokeLayout = unwind.InvokeThunkLayout{
	CallOffset: 24, CallLength: 4, LandingOffset: 36, RangeLength: 60,
}

const exceptionInvokeCFIPersonalityEncoding byte = unwind.PEIndirect | unwind.PEPcrel | unwind.PESdata4

type ExceptionInvokeConfig struct {
	FunctionAddress uint64
	Plan            *unwind.ExceptionBridgePlan
}

type ExceptionInvokeImage struct {
	FunctionAddress            uint64
	Personality                uint64
	PersonalityEncoding        byte
	EmittedPersonalityEncoding byte
	PersonalityAnchor          string
	Thunk                      unwind.InvokeThunk
	ThunkSymbol                string
	LSDASymbol                 string
	LSDA                       *unwind.BridgeLSDA
}

func generateExceptionInvokeThunks(configs []ExceptionInvokeConfig) (assembly []byte, normalized []ExceptionInvokeImage, err error) {
	ordered := append([]ExceptionInvokeConfig(nil), configs...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].FunctionAddress != ordered[j].FunctionAddress {
			return ordered[i].FunctionAddress < ordered[j].FunctionAddress
		}
		var left, right uint64
		if ordered[i].Plan != nil {
			left = ordered[i].Plan.Personality
		}
		if ordered[j].Plan != nil {
			right = ordered[j].Plan.Personality
		}
		return left < right
	})

	seenFunction := map[uint64]bool{}
	seenID := map[uint32]bool{}
	seenCall := map[[2]uint64]bool{}
	var anchors []string

	for _, cfg := range ordered {
		if cfg.FunctionAddress == 0 || cfg.Plan == nil || cfg.Plan.Personality == 0 || len(cfg.Plan.Thunks) == 0 {
			return nil, nil, fmt.Errorf("exception invoke requires a function address and non-empty personality plan")
		}
		if seenFunction[cfg.FunctionAddress] {
			return nil, nil, fmt.Errorf("duplicate exception invoke function 0x%x", cfg.FunctionAddress)
		}
		seenFunction[cfg.FunctionAddress] = true
		if cfg.Plan.PersonalityEncoding != unwind.PEPcrel|unwind.PESdata4 &&
			cfg.Plan.PersonalityEncoding != unwind.PEIndirect|unwind.PEPcrel|unwind.PESdata4 {
			return nil, nil, fmt.Errorf("exception personality encoding 0x%x is not supported by the runtime invoke wrapper", cfg.Plan.PersonalityEncoding)
		}
		anchor := fmt.Sprintf("vm_personality_anchor_%016x", cfg.FunctionAddress)
		anchors = append(anchors, anchor)

		thunks := append([]unwind.InvokeThunk(nil), cfg.Plan.Thunks...)
		sort.Slice(thunks, func(i, j int) bool {
			if thunks[i].OriginalPC != thunks[j].OriginalPC {
				return thunks[i].OriginalPC < thunks[j].OriginalPC
			}
			return thunks[i].ID < thunks[j].ID
		})
		for _, thunk := range thunks {
			if thunk.ID == 0 || thunk.OriginalPC < cfg.FunctionAddress || thunk.OriginalLandingPad < cfg.FunctionAddress {
				return nil, nil, fmt.Errorf("invoke thunk 0x%08x has invalid original call/landing identity", thunk.ID)
			}
			if seenID[thunk.ID] {
				return nil, nil, fmt.Errorf("duplicate invoke thunk ID 0x%08x", thunk.ID)
			}
			seenID[thunk.ID] = true
			callKey := [2]uint64{cfg.FunctionAddress, thunk.OriginalPC}
			if seenCall[callKey] {
				return nil, nil, fmt.Errorf("duplicate invoke call identity 0x%x", thunk.OriginalPC)
			}
			seenCall[callKey] = true
			bridge, err := unwind.BuildBridgeLSDA(cfg.Plan, thunk, exceptionInvokeLayout)
			if err != nil {
				return nil, nil, fmt.Errorf("build invoke thunk 0x%08x LSDA: %w", thunk.ID, err)
			}
			normalized = append(normalized, ExceptionInvokeImage{
				FunctionAddress: cfg.FunctionAddress, Personality: cfg.Plan.Personality,
				PersonalityEncoding: cfg.Plan.PersonalityEncoding, EmittedPersonalityEncoding: exceptionInvokeCFIPersonalityEncoding,
				PersonalityAnchor: anchor, Thunk: thunk, ThunkSymbol: fmt.Sprintf("vm_invoke_%08x", thunk.ID),
				LSDASymbol: fmt.Sprintf("vm_lsda_invoke_%08x", thunk.ID), LSDA: cloneBridgeLSDA(bridge),
			})
		}
	}

	var s strings.Builder
	s.WriteString("#include \"vm_abi.h\"\n")
	for _, anchor := range anchors {
		fmt.Fprintf(&s, ".section .rodata.invoke_meta,\"a\",%%progbits\n.p2align 3\n.global %s\n.hidden %s\n.type %s, %%object\n%s:\n.xword 0\n.size %s, .-%s\n", anchor, anchor, anchor, anchor, anchor, anchor)
	}
	for _, item := range normalized {
		fmt.Fprintf(&s, ".section .gcc_except_table.invoke,\"a\",%%progbits\n.p2align 2\n.global %s\n.hidden %s\n.type %s, %%object\n%s:\n", item.LSDASymbol, item.LSDASymbol, item.LSDASymbol, item.LSDASymbol)
		emitAssemblyBytes(&s, item.LSDA.Bytes)
		fmt.Fprintf(&s, ".size %s, .-%s\n", item.LSDASymbol, item.LSDASymbol)
	}
	for _, item := range normalized {
		returnLabel := fmt.Sprintf(".Linvoke_return_%08x", item.Thunk.ID)
		landingLabel := fmt.Sprintf(".Linvoke_landing_%08x", item.Thunk.ID)
		fmt.Fprintf(&s, ".section .text.invoke,\"ax\",%%progbits\n.p2align 2\n.global %s\n.hidden %s\n.type %s, %%function\n%s:\n", item.ThunkSymbol, item.ThunkSymbol, item.ThunkSymbol, item.ThunkSymbol)
		s.WriteString(".cfi_startproc\n")
		fmt.Fprintf(&s, ".cfi_personality 0x%02x, %s\n", item.EmittedPersonalityEncoding, item.PersonalityAnchor)
		fmt.Fprintf(&s, ".cfi_lsda 0x1b, %s\n", item.LSDASymbol)
		s.WriteString("bti c\npaciasp\n.cfi_negate_ra_state\nstp x29, x30, [sp, #-32]!\n.cfi_def_cfa_offset 32\n.cfi_offset x29, -32\n.cfi_offset x30, -24\nstr x19, [sp, #16]\n.cfi_offset x19, -16\nmov x29, sp\nmov x19, x0\nbl vm_native_call\nmov w0, #0\n")
		fmt.Fprintf(&s, "b %s\n%s:\n", returnLabel, landingLabel)
		s.WriteString("stp x0, x1, [x19, #VM_CTX_R]\nmov w0, #1\n")
		fmt.Fprintf(&s, "%s:\n", returnLabel)
		s.WriteString("ldr x19, [sp, #16]\n.cfi_restore x19\nldp x29, x30, [sp], #32\n.cfi_def_cfa_offset 0\n.cfi_restore x29\n.cfi_restore x30\nautiasp\n.cfi_negate_ra_state\nret\n.cfi_endproc\n")
		fmt.Fprintf(&s, ".size %s, .-%s\n", item.ThunkSymbol, item.ThunkSymbol)
	}
	s.WriteString(".section .note.gnu.property,\"a\",%note\n.p2align 3\n.long 4\n.long 16\n.long 5\n.asciz \"GNU\"\n.p2align 3\n.long 0xc0000000\n.long 4\n.long 3\n.long 0\n.section .note.GNU-stack,\"\",%progbits\n")
	return []byte(s.String()), normalized, nil
}

func emitAssemblyBytes(s *strings.Builder, data []byte) {
	if len(data) == 0 {
		s.WriteString(".byte 0\n")
		return
	}
	for offset := 0; offset < len(data); offset += 16 {
		end := offset + 16
		if end > len(data) {
			end = len(data)
		}
		s.WriteString(".byte ")
		for i := offset; i < end; i++ {
			if i > offset {
				s.WriteByte(',')
			}
			fmt.Fprintf(s, "0x%02x", data[i])
		}
		s.WriteByte('\n')
	}
}

func cloneBridgeLSDA(source *unwind.BridgeLSDA) *unwind.BridgeLSDA {
	if source == nil {
		return nil
	}
	return &unwind.BridgeLSDA{
		Bytes:       append([]byte(nil), source.Bytes...),
		Relocations: append([]unwind.LSDARelocation(nil), source.Relocations...),
	}
}
