package runtime

import (
	"debug/elf"
	"fmt"
	"sort"
	"strings"

	"github.com/vmpacker/internal/unwind"
)

var exceptionInvokeLayout = unwind.InvokeThunkLayout{
	CallOffset: 24, CallLength: 4, LandingOffset: 36, RangeLength: 60,
}

// Generated personalities and LSDAs always use direct PC-relative signed
// 32-bit references. Source indirectness is retained only where the LSDA type
// table requires the unwinder to dereference the original type-info slot.
const exceptionInvokeCFIPersonalityEncoding byte = unwind.PEPcrel | unwind.PESdata4
const exceptionInvokeCFILSDAEncoding byte = unwind.PEPcrel | unwind.PESdata4

type ExceptionRouteConfig struct {
	ThunkID              uint32
	FinalVMCallOffset    uint32
	FinalVMLandingOffset uint32
}

type ExceptionInvokeConfig struct {
	FunctionAddress uint64
	Plan            *unwind.ExceptionBridgePlan
	Routes          []ExceptionRouteConfig
}

type ExceptionInvokeImage struct {
	FunctionAddress            uint64
	Personality                uint64
	PersonalityEncoding        byte
	EmittedPersonalityEncoding byte
	PersonalityBridge          string
	Thunk                      unwind.InvokeThunk
	ThunkSymbol                string
	LSDASymbol                 string
	LSDA                       *unwind.BridgeLSDA
	FinalVMCallOffset          uint32
	FinalVMLandingOffset       uint32
}

func generateExceptionInvokeThunks(configs []ExceptionInvokeConfig) (header, assembly []byte, normalized []ExceptionInvokeImage, err error) {
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
	seenFinalCall := map[[2]uint64]bool{}

	for _, cfg := range ordered {
		if cfg.FunctionAddress == 0 || cfg.Plan == nil || cfg.Plan.Personality == 0 || len(cfg.Plan.Thunks) == 0 {
			return nil, nil, nil, fmt.Errorf("exception invoke requires a function address and non-empty personality plan")
		}
		if seenFunction[cfg.FunctionAddress] {
			return nil, nil, nil, fmt.Errorf("duplicate exception invoke function 0x%x", cfg.FunctionAddress)
		}
		seenFunction[cfg.FunctionAddress] = true
		if cfg.Plan.PersonalityEncoding != unwind.PEAbsptr &&
			cfg.Plan.PersonalityEncoding != unwind.PEPcrel|unwind.PESdata4 &&
			cfg.Plan.PersonalityEncoding != unwind.PEIndirect|unwind.PEPcrel|unwind.PESdata4 {
			return nil, nil, nil, fmt.Errorf("exception personality encoding 0x%x is not supported by the runtime invoke wrapper", cfg.Plan.PersonalityEncoding)
		}

		routeByID := make(map[uint32]ExceptionRouteConfig, len(cfg.Routes))
		for _, route := range cfg.Routes {
			if route.ThunkID == 0 || route.FinalVMCallOffset == 0 || route.FinalVMLandingOffset == 0 {
				return nil, nil, nil, fmt.Errorf("exception route for function 0x%x has an invalid zero identity or final VM boundary", cfg.FunctionAddress)
			}
			if _, duplicate := routeByID[route.ThunkID]; duplicate {
				return nil, nil, nil, fmt.Errorf("duplicate exception route for thunk 0x%08x", route.ThunkID)
			}
			routeByID[route.ThunkID] = route
		}

		thunks := append([]unwind.InvokeThunk(nil), cfg.Plan.Thunks...)
		sort.Slice(thunks, func(i, j int) bool {
			if thunks[i].OriginalPC != thunks[j].OriginalPC {
				return thunks[i].OriginalPC < thunks[j].OriginalPC
			}
			return thunks[i].ID < thunks[j].ID
		})
		if len(routeByID) != len(thunks) {
			return nil, nil, nil, fmt.Errorf("exception function 0x%x has %d thunks but %d final VM routes", cfg.FunctionAddress, len(thunks), len(routeByID))
		}

		emittedPlan := cloneExceptionBridgePlan(cfg.Plan)
		if emittedPlan.TypeEncoding != unwind.PEOmit {
			emittedPlan.TypeEncoding = unwind.PEPcrel | unwind.PESdata4 | (emittedPlan.TypeEncoding & unwind.PEIndirect)
		}
		personalityBridge := fmt.Sprintf("vm_personality_bridge_%016x", cfg.FunctionAddress)

		for _, thunk := range thunks {
			if thunk.ID == 0 || thunk.OriginalPC < cfg.FunctionAddress || thunk.OriginalLandingPad < cfg.FunctionAddress {
				return nil, nil, nil, fmt.Errorf("invoke thunk 0x%08x has invalid original call/landing identity", thunk.ID)
			}
			if seenID[thunk.ID] {
				return nil, nil, nil, fmt.Errorf("duplicate invoke thunk ID 0x%08x", thunk.ID)
			}
			seenID[thunk.ID] = true
			route, ok := routeByID[thunk.ID]
			if !ok {
				return nil, nil, nil, fmt.Errorf("invoke thunk 0x%08x has no final VM route", thunk.ID)
			}
			callKey := [2]uint64{cfg.FunctionAddress, uint64(route.FinalVMCallOffset)}
			if seenFinalCall[callKey] {
				return nil, nil, nil, fmt.Errorf("duplicate final exception call route function=0x%x vm=0x%x", cfg.FunctionAddress, route.FinalVMCallOffset)
			}
			seenFinalCall[callKey] = true

			bridge, err := unwind.BuildBridgeLSDA(emittedPlan, thunk, exceptionInvokeLayout)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("build invoke thunk 0x%08x LSDA: %w", thunk.ID, err)
			}
			normalized = append(normalized, ExceptionInvokeImage{
				FunctionAddress: cfg.FunctionAddress, Personality: cfg.Plan.Personality,
				PersonalityEncoding:        cfg.Plan.PersonalityEncoding,
				EmittedPersonalityEncoding: exceptionInvokeCFIPersonalityEncoding,
				PersonalityBridge:          personalityBridge, Thunk: thunk,
				ThunkSymbol:          fmt.Sprintf("vm_invoke_%08x", thunk.ID),
				LSDASymbol:           fmt.Sprintf("vm_lsda_invoke_%08x", thunk.ID),
				LSDA:                 cloneBridgeLSDA(bridge),
				FinalVMCallOffset:    route.FinalVMCallOffset,
				FinalVMLandingOffset: route.FinalVMLandingOffset,
			})
		}
	}

	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].FunctionAddress != normalized[j].FunctionAddress {
			return normalized[i].FunctionAddress < normalized[j].FunctionAddress
		}
		if normalized[i].FinalVMCallOffset != normalized[j].FinalVMCallOffset {
			return normalized[i].FinalVMCallOffset < normalized[j].FinalVMCallOffset
		}
		return normalized[i].Thunk.ID < normalized[j].Thunk.ID
	})

	header = []byte(generateExceptionInvokeHeader(len(normalized)))
	var source strings.Builder
	source.WriteString("#include \"vm_abi.h\"\n")

	personalityByBridge := make(map[string]uint64)
	for _, item := range normalized {
		personalityByBridge[item.PersonalityBridge] = item.Personality
	}
	bridges := make([]string, 0, len(personalityByBridge))
	for bridge := range personalityByBridge {
		bridges = append(bridges, bridge)
	}
	sort.Strings(bridges)
	for _, bridge := range bridges {
		targetSymbol := ".L" + bridge + "_target"
		deltaSymbol := ".L" + bridge + "_delta"
		fmt.Fprintf(&source, ".set %s, 0x%x\n", targetSymbol, personalityByBridge[bridge])
		fmt.Fprintf(&source, ".section .text.invoke_personality,\"ax\",%%progbits\n.p2align 2\n.global %s\n.hidden %s\n.type %s, %%function\n%s:\n", bridge, bridge, bridge, bridge)
		source.WriteString(".cfi_startproc\nbti c\n")
		fmt.Fprintf(&source, "adr x17, %s\nldrsw x16, [x17]\nadd x16, x17, x16\nbr x16\n.cfi_endproc\n.size %s, .-%s\n.p2align 2\n%s:\n.word %s - %s\n", deltaSymbol, bridge, bridge, deltaSymbol, targetSymbol, deltaSymbol)
	}

	for _, item := range normalized {
		fmt.Fprintf(&source, ".section .gcc_except_table.invoke,\"a\",%%progbits\n.p2align 2\n.global %s\n.hidden %s\n.type %s, %%object\n%s:\n", item.LSDASymbol, item.LSDASymbol, item.LSDASymbol, item.LSDASymbol)
		if err := emitRelocatableLSDA(&source, item); err != nil {
			return nil, nil, nil, err
		}
		fmt.Fprintf(&source, ".size %s, .-%s\n", item.LSDASymbol, item.LSDASymbol)
	}

	for _, item := range normalized {
		returnLabel := fmt.Sprintf(".Linvoke_return_%08x", item.Thunk.ID)
		landingLabel := fmt.Sprintf(".Linvoke_landing_%08x", item.Thunk.ID)
		fmt.Fprintf(&source, ".section .text.invoke,\"ax\",%%progbits\n.p2align 2\n.global %s\n.hidden %s\n.type %s, %%function\n%s:\n", item.ThunkSymbol, item.ThunkSymbol, item.ThunkSymbol, item.ThunkSymbol)
		source.WriteString(".cfi_startproc\n")
		fmt.Fprintf(&source, ".cfi_personality 0x%02x, %s\n", item.EmittedPersonalityEncoding, item.PersonalityBridge)
		fmt.Fprintf(&source, ".cfi_lsda 0x%02x, %s\n", exceptionInvokeCFILSDAEncoding, item.LSDASymbol)
		source.WriteString("bti c\npaciasp\n.cfi_negate_ra_state\nstp x29, x30, [sp, #-32]!\n.cfi_def_cfa_offset 32\n.cfi_offset x29, -32\n.cfi_offset x30, -24\nstr x19, [sp, #16]\n.cfi_offset x19, -16\nmov x29, sp\nmov x19, x0\nbl vm_native_call\nmov w0, #0\n")
		fmt.Fprintf(&source, "b %s\n%s:\n", returnLabel, landingLabel)
		source.WriteString("stp x0, x1, [x19, #VM_CTX_R]\nmov w0, #1\n")
		fmt.Fprintf(&source, "%s:\n", returnLabel)
		source.WriteString("ldr x19, [sp, #16]\n.cfi_restore x19\nldp x29, x30, [sp], #32\n.cfi_def_cfa_offset 0\n.cfi_restore x29\n.cfi_restore x30\nautiasp\n.cfi_negate_ra_state\nret\n.cfi_endproc\n")
		fmt.Fprintf(&source, ".size %s, .-%s\n", item.ThunkSymbol, item.ThunkSymbol)
	}

	if len(normalized) != 0 {
		source.WriteString(".section .rodata.invoke_routes,\"a\",%progbits\n.p2align 3\n.global vm_invoke_routes\n.hidden vm_invoke_routes\n.type vm_invoke_routes, %object\nvm_invoke_routes:\n")
		for _, item := range normalized {
			fmt.Fprintf(&source, ".xword 0x%x\n.word 0x%x\n.word 0x%x\n.word %s - .\n.word 0\n", item.FunctionAddress, item.FinalVMCallOffset, item.FinalVMLandingOffset, item.ThunkSymbol)
		}
		source.WriteString(".size vm_invoke_routes, .-vm_invoke_routes\n.p2align 3\n.global vm_invoke_route_count\n.hidden vm_invoke_route_count\n.type vm_invoke_route_count, %object\nvm_invoke_route_count:\n")
		fmt.Fprintf(&source, ".xword %d\n.size vm_invoke_route_count, .-vm_invoke_route_count\n", len(normalized))
	}
	source.WriteString(".section .note.gnu.property,\"a\",%note\n.p2align 3\n.long 4\n.long 16\n.long 5\n.asciz \"GNU\"\n.p2align 3\n.long 0xc0000000\n.long 4\n.long 3\n.long 0\n.section .note.GNU-stack,\"\",%progbits\n")
	return header, []byte(source.String()), normalized, nil
}

func generateExceptionInvokeHeader(routeCount int) string {
	var header strings.Builder
	header.WriteString("#ifndef VM_INVOKE_H\n#define VM_INVOKE_H\n\n#include \"vm_call.h\"\n\n")
	header.WriteString("typedef struct {\n  u64 function_file_va;\n  u32 call_vm_offset;\n  u32 landing_vm_offset;\n  i32 thunk_delta;\n  u32 reserved;\n} vm_invoke_route_t;\n\n")
	header.WriteString("_Static_assert(sizeof(vm_invoke_route_t) == 24, \"invoke route ABI\");\n")
	fmt.Fprintf(&header, "#define VM_INVOKE_ROUTE_COUNT %du\n", routeCount)
	header.WriteString("#define VM_INVOKE_NONE 0\n#define VM_INVOKE_NORMAL 1\n#define VM_INVOKE_LANDING 2\n#define VM_INVOKE_ERROR -1\n\n")
	if routeCount == 0 {
		header.WriteString(`static inline int vm_try_exception_invoke(vm_ctx_t *vm, u64 target,
                                          u32 call_vm_offset) {
  (void)vm;
  (void)target;
  (void)call_vm_offset;
  return VM_INVOKE_NONE;
}

#endif /* VM_INVOKE_H */
`)
		return header.String()
	}
	header.WriteString("extern const vm_invoke_route_t vm_invoke_routes[] __attribute__((visibility(\"hidden\")));\n")
	header.WriteString("extern const u64 vm_invoke_route_count __attribute__((visibility(\"hidden\")));\n")
	header.WriteString("typedef int (*vm_invoke_fn)(vm_ctx_t *, u64);\n\n")
	header.WriteString(`static inline int vm_invoke_key_compare(const vm_invoke_route_t *route,
                                        u64 function_file_va, u32 call_vm_offset) {
  if (route->function_file_va < function_file_va)
    return -1;
  if (route->function_file_va > function_file_va)
    return 1;
  if (route->call_vm_offset < call_vm_offset)
    return -1;
  if (route->call_vm_offset > call_vm_offset)
    return 1;
  return 0;
}

static inline int vm_try_exception_invoke(vm_ctx_t *vm, u64 target,
                                          u32 call_vm_offset) {
  u64 count = *(volatile const u64 *)&vm_invoke_route_count;
  if (count != VM_INVOKE_ROUTE_COUNT) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return VM_INVOKE_ERROR;
  }
  if (count == 0)
    return VM_INVOKE_NONE;
  if (!vm->reverse || target == 0 || call_vm_offset > vm->bc_len) {
    vm_fault_set(vm, VM_FAULT_CONTROL);
    return VM_INVOKE_ERROR;
  }

  u64 bias;
  if (!vm_file_bias(vm, &bias) || vm->func_addr < bias) {
    vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
    return VM_INVOKE_ERROR;
  }
  u64 function_file_va = vm->func_addr - bias;
  u32 lo = 0;
  u32 hi = (u32)count;
  while (lo < hi) {
    u32 mid = lo + ((hi - lo) >> 1);
    const vm_invoke_route_t *route = &vm_invoke_routes[mid];
    int comparison = vm_invoke_key_compare(route, function_file_va,
                                                   call_vm_offset);
    if (comparison < 0) {
      lo = mid + 1;
    } else if (comparison > 0) {
      hi = mid;
    } else {
      if (route->reserved != 0 || route->landing_vm_offset == 0 ||
          route->landing_vm_offset > vm->bc_len) {
        vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
        return VM_INVOKE_ERROR;
      }
      u64 field = (u64)&route->thunk_delta;
      i64 delta = route->thunk_delta;
      u64 thunk;
      if (delta >= 0) {
        if ((u64)delta > ~(u64)0 - field) {
          vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
          return VM_INVOKE_ERROR;
        }
        thunk = field + (u64)delta;
      } else {
        u64 amount = (u64)(-(delta + 1)) + 1u;
        if (amount > field) {
          vm_fault_set(vm, VM_FAULT_DESCRIPTOR);
          return VM_INVOKE_ERROR;
        }
        thunk = field - amount;
      }
      int landed = ((vm_invoke_fn)thunk)(vm, target);
      if (vm->fault)
        return VM_INVOKE_ERROR;
      if (landed == 0)
        return VM_INVOKE_NORMAL;
      if (landed != 1) {
        vm_fault_set(vm, VM_FAULT_INTERNAL);
        return VM_INVOKE_ERROR;
      }
      vm->pc = route->landing_vm_offset;
      return VM_INVOKE_LANDING;
    }
  }
  return VM_INVOKE_NONE;
}

#endif /* VM_INVOKE_H */
`)
	return header.String()
}

func emitRelocatableLSDA(source *strings.Builder, item ExceptionInvokeImage) error {
	if item.LSDA == nil || len(item.LSDA.Bytes) == 0 {
		return fmt.Errorf("invoke thunk 0x%08x has no generated LSDA", item.Thunk.ID)
	}
	relocations := append([]unwind.LSDARelocation(nil), item.LSDA.Relocations...)
	sort.Slice(relocations, func(i, j int) bool { return relocations[i].Offset < relocations[j].Offset })
	cursor := 0
	for index, relocation := range relocations {
		if relocation.Encoding&0x7f != unwind.PEPcrel|unwind.PESdata4 ||
			relocation.Indirect != (relocation.Encoding&unwind.PEIndirect != 0) {
			return fmt.Errorf("invoke thunk 0x%08x LSDA relocation %d has unsupported encoding 0x%x", item.Thunk.ID, index, relocation.Encoding)
		}
		offset := int(relocation.Offset)
		if offset < cursor || offset > len(item.LSDA.Bytes)-4 {
			return fmt.Errorf("invoke thunk 0x%08x LSDA relocation %d exceeds or overlaps output", item.Thunk.ID, index)
		}
		emitAssemblyBytes(source, item.LSDA.Bytes[cursor:offset])
		target := fmt.Sprintf(".Lvm_lsda_target_%08x_%d", item.Thunk.ID, index)
		fmt.Fprintf(source, ".set %s, 0x%x\n.word %s - .\n", target, relocation.Target, target)
		cursor = offset + 4
	}
	emitAssemblyBytes(source, item.LSDA.Bytes[cursor:])
	return nil
}

func validateExceptionInvokeImage(image *Image, invokes []ExceptionInvokeImage) error {
	if image == nil {
		return fmt.Errorf("runtime image is required for exception invoke validation")
	}
	if len(invokes) == 0 {
		return nil
	}
	symbols := make(map[string]*Symbol, len(image.Symbols))
	for index := range image.Symbols {
		symbol := &image.Symbols[index]
		if symbol.Name != "" {
			if _, duplicate := symbols[symbol.Name]; duplicate {
				return fmt.Errorf("runtime has duplicate generated symbol %q", symbol.Name)
			}
			symbols[symbol.Name] = symbol
		}
	}
	validateStorage := func(name string, wantType elf.SymType, wantSize uint64, executable bool) error {
		symbol := symbols[name]
		if symbol == nil {
			return fmt.Errorf("runtime is missing generated exception symbol %q", name)
		}
		if elf.ST_TYPE(symbol.Info) != wantType {
			return fmt.Errorf("generated exception symbol %q has unexpected ELF type", name)
		}
		if wantSize != 0 && symbol.Size != wantSize {
			return fmt.Errorf("generated exception symbol %q has size %d; want %d", name, symbol.Size, wantSize)
		}
		if int(symbol.Section) <= 0 || int(symbol.Section) >= len(image.Sections) {
			return fmt.Errorf("generated exception symbol %q has invalid section", name)
		}
		section := image.Sections[symbol.Section]
		if section.Flags&elf.SHF_ALLOC == 0 {
			return fmt.Errorf("generated exception symbol %q is not allocatable", name)
		}
		if executable && section.Flags&elf.SHF_EXECINSTR == 0 {
			return fmt.Errorf("generated exception function %q is not executable", name)
		}
		if !executable && section.Flags&elf.SHF_EXECINSTR != 0 {
			return fmt.Errorf("generated exception object %q is executable", name)
		}
		return nil
	}

	if err := validateStorage("vm_invoke_routes", elf.STT_OBJECT, uint64(len(invokes))*24, false); err != nil {
		return err
	}
	if err := validateStorage("vm_invoke_route_count", elf.STT_OBJECT, 8, false); err != nil {
		return err
	}
	seenBridge := map[string]bool{}
	for _, item := range invokes {
		if item.EmittedPersonalityEncoding != exceptionInvokeCFIPersonalityEncoding {
			return fmt.Errorf("generated exception invoke %q has unexpected personality encoding 0x%x", item.ThunkSymbol, item.EmittedPersonalityEncoding)
		}
		if err := validateStorage(item.ThunkSymbol, elf.STT_FUNC, 0, true); err != nil {
			return err
		}
		if item.LSDA == nil || len(item.LSDA.Bytes) == 0 {
			return fmt.Errorf("generated exception invoke %q has no LSDA metadata", item.ThunkSymbol)
		}
		if err := validateStorage(item.LSDASymbol, elf.STT_OBJECT, uint64(len(item.LSDA.Bytes)), false); err != nil {
			return err
		}
		if !seenBridge[item.PersonalityBridge] {
			if err := validateStorage(item.PersonalityBridge, elf.STT_FUNC, 0, true); err != nil {
				return err
			}
			seenBridge[item.PersonalityBridge] = true
		}
	}
	return nil
}

func emitAssemblyBytes(source *strings.Builder, data []byte) {
	if len(data) == 0 {
		return
	}
	for offset := 0; offset < len(data); offset += 16 {
		end := offset + 16
		if end > len(data) {
			end = len(data)
		}
		source.WriteString(".byte ")
		for i := offset; i < end; i++ {
			if i > offset {
				source.WriteByte(',')
			}
			fmt.Fprintf(source, "0x%02x", data[i])
		}
		source.WriteByte('\n')
	}
}

func cloneExceptionBridgePlan(source *unwind.ExceptionBridgePlan) *unwind.ExceptionBridgePlan {
	if source == nil {
		return nil
	}
	result := &unwind.ExceptionBridgePlan{
		Personality: source.Personality, PersonalityEncoding: source.PersonalityEncoding,
		TypeEncoding: source.TypeEncoding, TypeInfos: make(map[uint64]unwind.TypeInfo, len(source.TypeInfos)),
		ActionTable:    append([]byte(nil), source.ActionTable...),
		TypeIndexTable: append([]byte(nil), source.TypeIndexTable...),
		Thunks:         append([]unwind.InvokeThunk(nil), source.Thunks...),
	}
	for key, value := range source.TypeInfos {
		result.TypeInfos[key] = value
	}
	for index := range result.Thunks {
		result.Thunks[index].Actions = append([]unwind.ActionRecord(nil), result.Thunks[index].Actions...)
		for action := range result.Thunks[index].Actions {
			result.Thunks[index].Actions[action].FilterTypes = append([]uint64(nil), result.Thunks[index].Actions[action].FilterTypes...)
		}
	}
	return result
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
