package unwind

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

type NativeCallLocation struct {
	OriginalPC uint64
	VMOffset   uint32
}

type InvokeThunk struct {
	ID                 uint32
	OriginalPC         uint64
	OriginalLandingPad uint64
	VMCallOffset       uint32
	VMLandingPad       uint32
	Action             uint64
	Actions            []ActionRecord
}

type ExceptionBridgePlan struct {
	Personality         uint64
	PersonalityEncoding byte
	TypeEncoding        byte
	TypeInfos           map[uint64]TypeInfo
	ActionTable         []byte
	TypeIndexTable      []byte
	Thunks              []InvokeThunk
}

// PlanExceptionBridge associates every potentially throwing native call with
// exactly one original LSDA range and gives it a content-addressed invoke
// thunk. The plan retains the original Android C++ personality and resolved
// type-info targets; final addresses are deliberately deferred to RewritePlan.
func PlanExceptionBridge(cie *CIE, fde *FDE, lsda *LSDA, mapped []MappedCallSite, calls []NativeCallLocation) (*ExceptionBridgePlan, error) {
	if cie == nil || fde == nil || lsda == nil {
		return nil, fmt.Errorf("CIE, FDE, and LSDA are required")
	}
	if cie.Personality == nil || cie.PersonalityEncoding == PEOmit {
		return nil, fmt.Errorf("exception bridge requires the original personality")
	}
	if fde.LSDA == nil {
		return nil, fmt.Errorf("exception bridge FDE has no LSDA")
	}
	if len(mapped) != len(lsda.CallSites) {
		return nil, fmt.Errorf("mapped/original call-site count mismatch")
	}
	if len(calls) == 0 {
		return nil, fmt.Errorf("exception bridge has no native call locations")
	}
	for i, site := range lsda.CallSites {
		if site.Length == 0 || site.Start > math.MaxUint64-site.Length {
			return nil, fmt.Errorf("call-site %d has an invalid range", i)
		}
		if i > 0 {
			previous := lsda.CallSites[i-1]
			if previous.Start+previous.Length > site.Start {
				return nil, fmt.Errorf("call-site ranges %d and %d overlap", i-1, i)
			}
		}
		if site.Action != 0 && len(lsda.ActionChains[site.Action]) == 0 {
			return nil, fmt.Errorf("call-site %d references missing action 0x%x", i, site.Action)
		}
		if site.LandingPad != 0 && mapped[i].VMLandingPad == 0 {
			return nil, fmt.Errorf("call-site %d landing pad has no VM target", i)
		}
	}

	plan := &ExceptionBridgePlan{
		Personality: *cie.Personality, PersonalityEncoding: cie.PersonalityEncoding,
		TypeEncoding: lsda.TypeEncoding, TypeInfos: make(map[uint64]TypeInfo, len(lsda.TypeInfos)),
		ActionTable: append([]byte(nil), lsda.ActionTable...), TypeIndexTable: append([]byte(nil), lsda.TypeIndexTable...),
	}
	for index, info := range lsda.TypeInfos {
		plan.TypeInfos[index] = info
	}

	sort.Slice(calls, func(i, j int) bool { return calls[i].OriginalPC < calls[j].OriginalPC })
	seenPC := map[uint64]bool{}
	seenID := map[uint32]bool{}
	for _, call := range calls {
		if seenPC[call.OriginalPC] {
			return nil, fmt.Errorf("duplicate native call PC 0x%x", call.OriginalPC)
		}
		seenPC[call.OriginalPC] = true
		siteIndex := -1
		for i, site := range lsda.CallSites {
			if call.OriginalPC >= site.Start && call.OriginalPC < site.Start+site.Length {
				if siteIndex >= 0 {
					return nil, fmt.Errorf("native call PC 0x%x matches multiple call sites", call.OriginalPC)
				}
				siteIndex = i
			}
		}
		if siteIndex < 0 {
			continue // a call outside an EH range retains the ordinary native bridge
		}
		site := lsda.CallSites[siteIndex]
		if site.LandingPad == 0 {
			continue // unwind-through range; no local landing wrapper is required
		}
		thunk := InvokeThunk{
			OriginalPC: call.OriginalPC, OriginalLandingPad: site.LandingPad,
			VMCallOffset: call.VMOffset, VMLandingPad: mapped[siteIndex].VMLandingPad, Action: site.Action,
			Actions: append([]ActionRecord(nil), lsda.ActionChains[site.Action]...),
		}
		thunk.ID = invokeThunkID(*cie.Personality, fde.Offset, thunk)
		if seenID[thunk.ID] {
			return nil, fmt.Errorf("invoke thunk identifier collision 0x%08x", thunk.ID)
		}
		seenID[thunk.ID] = true
		plan.Thunks = append(plan.Thunks, thunk)
	}
	if len(plan.Thunks) == 0 {
		return nil, fmt.Errorf("no native call requires an exception landing bridge")
	}
	return plan, nil
}

func invokeThunkID(personality, fdeOffset uint64, thunk InvokeThunk) uint32 {
	h := sha256.New()
	h.Write([]byte("vmpacker-invoke-thunk-v1\x00"))
	var b [8]byte
	for _, value := range []uint64{personality, fdeOffset, thunk.OriginalPC, thunk.OriginalLandingPad, uint64(thunk.VMCallOffset), uint64(thunk.VMLandingPad), thunk.Action} {
		binary.LittleEndian.PutUint64(b[:], value)
		h.Write(b[:])
	}
	sum := h.Sum(nil)
	return binary.LittleEndian.Uint32(sum[:4])
}

type InvokeThunkLayout struct {
	CallOffset    uint32
	CallLength    uint32
	LandingOffset uint32
	RangeLength   uint32
}

type LSDARelocation struct {
	Offset   uint32
	Encoding byte
	Target   uint64
	Indirect bool
}

type BridgeLSDA struct {
	Bytes       []byte
	Relocations []LSDARelocation
}

// ApplyRelocations materializes a BridgeLSDA at its final virtual address.
// It is intentionally separate from BuildBridgeLSDA so planning never depends
// on provisional layout addresses.
func (bridge *BridgeLSDA) ApplyRelocations(baseVA uint64) ([]byte, error) {
	if bridge == nil {
		return nil, fmt.Errorf("bridge LSDA is required")
	}
	result := append([]byte(nil), bridge.Bytes...)
	for _, relocation := range bridge.Relocations {
		size, err := fixedEncodingSize(relocation.Encoding, 8)
		if err != nil {
			return nil, err
		}
		offset := int(relocation.Offset)
		if offset < 0 || offset > len(result)-size {
			return nil, fmt.Errorf("LSDA relocation offset 0x%x exceeds output", relocation.Offset)
		}
		application := relocation.Encoding & 0x70
		var unsigned uint64
		var signed int64
		isSigned := relocation.Encoding&0x08 != 0
		switch application {
		case 0:
			unsigned = relocation.Target
			if isSigned {
				if relocation.Target > math.MaxInt64 {
					return nil, fmt.Errorf("absolute signed LSDA relocation overflows")
				}
				signed = int64(relocation.Target)
			}
		case PEPcrel:
			field := baseVA + uint64(offset)
			if field < baseVA {
				return nil, fmt.Errorf("LSDA relocation field address overflows")
			}
			if relocation.Target >= field {
				delta := relocation.Target - field
				if delta > math.MaxInt64 {
					return nil, fmt.Errorf("positive LSDA PC-relative relocation overflows")
				}
				signed = int64(delta)
			} else {
				delta := field - relocation.Target
				if delta > 1<<63 {
					return nil, fmt.Errorf("negative LSDA PC-relative relocation overflows")
				}
				signed = -int64(delta)
			}
			unsigned = uint64(signed)
		default:
			return nil, fmt.Errorf("unsupported LSDA relocation application 0x%x", application)
		}
		if isSigned {
			if !fitsSigned(signed, size*8) {
				return nil, fmt.Errorf("signed %d-bit LSDA relocation is out of range", size*8)
			}
			unsigned = uint64(signed)
		} else if size < 8 && unsigned >= uint64(1)<<(size*8) {
			return nil, fmt.Errorf("unsigned %d-bit LSDA relocation is out of range", size*8)
		}
		switch size {
		case 2:
			binary.LittleEndian.PutUint16(result[offset:], uint16(unsigned))
		case 4:
			binary.LittleEndian.PutUint32(result[offset:], uint32(unsigned))
		case 8:
			binary.LittleEndian.PutUint64(result[offset:], unsigned)
		default:
			return nil, fmt.Errorf("unsupported LSDA relocation width %d", size)
		}
	}
	return result, nil
}

func fitsSigned(value int64, bits int) bool {
	if bits >= 64 {
		return true
	}
	min := -(int64(1) << (bits - 1))
	max := (int64(1) << (bits - 1)) - 1
	return value >= min && value <= max
}

// BuildBridgeLSDA emits one single-call LSDA while retaining the original
// action graph and type/filter tables. Type pointers remain explicit
// relocations so the Phase 8 writer must resolve them after final layout.
func BuildBridgeLSDA(plan *ExceptionBridgePlan, thunk InvokeThunk, layout InvokeThunkLayout) (*BridgeLSDA, error) {
	if plan == nil || layout.CallLength == 0 || layout.RangeLength == 0 {
		return nil, fmt.Errorf("bridge plan and non-empty thunk layout are required")
	}
	if layout.CallOffset > layout.RangeLength-layout.CallLength || layout.LandingOffset == 0 || layout.LandingOffset >= layout.RangeLength {
		return nil, fmt.Errorf("invoke thunk call/landing range is invalid")
	}
	if thunk.Action != 0 && len(thunk.Actions) == 0 {
		return nil, fmt.Errorf("invoke thunk action graph is missing")
	}
	typeSize := 0
	var err error
	if plan.TypeEncoding != PEOmit {
		typeSize, err = fixedEncodingSize(plan.TypeEncoding, 8)
		if err != nil {
			return nil, err
		}
		application := plan.TypeEncoding & 0x70
		if application != 0 && application != PEPcrel {
			return nil, fmt.Errorf("bridge type encoding application 0x%x requires unavailable layout bases", application)
		}
	}
	maxType := uint64(0)
	for index := range plan.TypeInfos {
		if index > maxType {
			maxType = index
		}
	}
	if typeSize != 0 && maxType > uint64(math.MaxInt/typeSize) {
		return nil, fmt.Errorf("bridge type table is too large")
	}

	callTable := make([]byte, 0, 16)
	callTable = binary.LittleEndian.AppendUint32(callTable, layout.CallOffset)
	callTable = binary.LittleEndian.AppendUint32(callTable, layout.CallLength)
	callTable = binary.LittleEndian.AppendUint32(callTable, layout.LandingOffset)
	callTable = appendULEB(callTable, thunk.Action)
	callLength := appendULEB(nil, uint64(len(callTable)))
	typeBytes := int(maxType) * typeSize

	typeOffset := uint64(0)
	var typeOffsetBytes []byte
	for iteration := 0; iteration < 8; iteration++ {
		typeOffsetBytes = appendULEB(nil, typeOffset)
		headerLength := 2 + len(typeOffsetBytes) + 1 + len(callLength)
		typeBase := headerLength + len(callTable) + len(plan.ActionTable) + typeBytes
		want := uint64(typeBase - (2 + len(typeOffsetBytes)))
		if want == typeOffset {
			break
		}
		typeOffset = want
	}
	typeOffsetBytes = appendULEB(nil, typeOffset)

	result := &BridgeLSDA{}
	result.Bytes = append(result.Bytes, PEOmit, plan.TypeEncoding)
	if plan.TypeEncoding != PEOmit {
		result.Bytes = append(result.Bytes, typeOffsetBytes...)
	}
	result.Bytes = append(result.Bytes, PEUdata4)
	result.Bytes = append(result.Bytes, callLength...)
	result.Bytes = append(result.Bytes, callTable...)
	result.Bytes = append(result.Bytes, plan.ActionTable...)
	typeTableStart := len(result.Bytes)
	result.Bytes = append(result.Bytes, make([]byte, typeBytes)...)
	typeBase := len(result.Bytes)
	for index := uint64(1); index <= maxType; index++ {
		info, ok := plan.TypeInfos[index]
		if !ok {
			return nil, fmt.Errorf("bridge type table index %d is missing", index)
		}
		offset := typeBase - int(index)*typeSize
		if info.Address != 0 {
			result.Relocations = append(result.Relocations, LSDARelocation{
				Offset: uint32(offset), Encoding: plan.TypeEncoding,
				Target: info.Address, Indirect: info.Indirect,
			})
		}
	}
	if typeTableStart+typeBytes != typeBase {
		panic("unwind bridge invariant: type table size")
	}
	result.Bytes = append(result.Bytes, plan.TypeIndexTable...)
	return result, nil
}

func appendULEB(dst []byte, value uint64) []byte {
	for {
		b := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if value == 0 {
			return dst
		}
	}
}
