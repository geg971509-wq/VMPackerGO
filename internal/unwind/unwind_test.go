package unwind

import (
	"encoding/binary"
	"testing"
)

func TestDecodePointerFormatsAndRelativeBases(t *testing.T) {
	data := []byte{0x7f, 0x78, 0xfc, 0xff, 0xff, 0xff}
	offset := 0
	value, err := DecodePointer(data, &offset, PEUleb128, binary.LittleEndian, 8, Bases{})
	if err != nil || value != 127 || offset != 1 {
		t.Fatalf("ULEB value=%d offset=%d err=%v", value, offset, err)
	}
	value, err = DecodePointer(data, &offset, PESleb128, binary.LittleEndian, 8, Bases{})
	if err != nil || value != ^uint64(7) || offset != 2 {
		t.Fatalf("SLEB value=0x%x offset=%d err=%v", value, offset, err)
	}
	value, err = DecodePointer(data, &offset, PEPcrel|PESdata4, binary.LittleEndian, 8, Bases{Field: 0x2000})
	if err != nil || value != 0x1ffe {
		t.Fatalf("PC-relative value=0x%x err=%v", value, err)
	}
	if _, err := DecodePointer([]byte{0}, new(int), PEIndirect|PEUdata4, binary.LittleEndian, 8, Bases{}); err == nil {
		t.Fatal("indirect encoding was accepted without target memory")
	}
}

func TestParseEHFrameCIEAndFDE(t *testing.T) {
	const sectionVA = uint64(0x1000)
	cieContent := []byte{1, 'z', 'R', 0, 1, 0x78, 30, 1, PEPcrel | PESdata4, 0x0c}
	cieBody := append([]byte{0, 0, 0, 0}, cieContent...)
	data := appendLength(nil, cieBody)
	fdeOffset := len(data)
	idFieldOffset := fdeOffset + 4
	fdeContentOffset := fdeOffset + 8
	delta := int32(0x2000 - (sectionVA + uint64(fdeContentOffset)))
	fdeBody := make([]byte, 4)
	binary.LittleEndian.PutUint32(fdeBody, uint32(idFieldOffset))
	encoded := make([]byte, 4)
	binary.LittleEndian.PutUint32(encoded, uint32(delta))
	fdeBody = append(fdeBody, encoded...)
	binary.LittleEndian.PutUint32(encoded, 0x40)
	fdeBody = append(fdeBody, encoded...)
	fdeBody = append(fdeBody, 0, 0x0c)
	data = appendLength(data, fdeBody)
	data = append(data, 0, 0, 0, 0)

	frame, err := ParseEHFrame(data, sectionVA, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	cie := frame.CIEs[0]
	if cie == nil || cie.CodeAlignment != 1 || cie.DataAlignment != -8 || cie.ReturnRegister != 30 || cie.FDEEncoding != PEPcrel|PESdata4 {
		t.Fatalf("CIE=%+v", cie)
	}
	if len(frame.FDEs) != 1 || frame.FDEs[0].InitialLocation != 0x2000 || frame.FDEs[0].AddressRange != 0x40 || frame.FDEs[0].CIEOffset != 0 {
		t.Fatalf("FDEs=%+v", frame.FDEs)
	}
}

func TestParseLSDAAndMapCallSites(t *testing.T) {
	data := []byte{PEOmit, PEOmit, PEUleb128, 4, 0x10, 4, 0x20, 1, 0, 0}
	lsda, err := ParseLSDA(data, 0x3000, 0x1000, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(lsda.CallSites) != 1 || lsda.CallSites[0].Start != 0x1010 || lsda.CallSites[0].Length != 4 || lsda.CallSites[0].LandingPad != 0x1020 {
		t.Fatalf("call sites=%+v", lsda.CallSites)
	}
	mapped, err := MapCallSites(lsda, func(pc uint64) (uint32, bool) {
		if pc < 0x1000 || pc > 0x1100 {
			return 0, false
		}
		return uint32((pc - 0x1000) * 2), true
	})
	if err != nil || len(mapped) != 1 || mapped[0].VMStart != 0x20 || mapped[0].VMLength != 8 || mapped[0].VMLandingPad != 0x40 || mapped[0].Action != 1 {
		t.Fatalf("mapped=%+v err=%v", mapped, err)
	}
}

func TestParseLSDATypedCatchActionsAndRelocatableTypeInfo(t *testing.T) {
	const address = uint64(0x3000)
	const typeAddress = uint64(0x5000)
	data := []byte{
		PEOmit,
		PEPcrel | PESdata4,
		12, // type table base is byte 15, measured after this ULEB
		PEUleb128,
		4,          // call-site table length
		0, 4, 8, 1, // start, length, landing, action
		1, 0, // action 1: catch type index 1, end of chain
		0, 0, 0, 0, // type entry 1, filled below
	}
	delta := int32(typeAddress - (address + 11))
	binary.LittleEndian.PutUint32(data[11:15], uint32(delta))
	lsda, err := ParseLSDA(data, address, 0x1000, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	chain := lsda.ActionChains[1]
	if len(chain) != 1 || chain[0].TypeFilter != 1 || chain[0].Next != 0 {
		t.Fatalf("action chain=%+v", chain)
	}
	info, ok := lsda.TypeInfos[1]
	if !ok || info.Address != typeAddress || info.Indirect || len(lsda.ActionTable) != 2 {
		t.Fatalf("type=%+v ok=%v action=%x", info, ok, lsda.ActionTable)
	}
}

func TestPlanExceptionBridgeAndRebuildSingleCallLSDA(t *testing.T) {
	const address = uint64(0x3000)
	const typeAddress = uint64(0x5000)
	data := []byte{PEOmit, PEPcrel | PESdata4, 12, PEUleb128, 4, 0, 4, 8, 1, 1, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(data[11:15], uint32(int32(typeAddress-(address+11))))
	lsda, err := ParseLSDA(data, address, 0x1000, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	personality := uint64(0x9000)
	lsdaAddress := address
	cie := &CIE{Personality: &personality, PersonalityEncoding: PEIndirect | PEPcrel | PESdata4}
	fde := &FDE{Offset: 0x40, LSDA: &lsdaAddress}
	mapped := []MappedCallSite{{VMStart: 0x20, VMLength: 8, VMLandingPad: 0x80, Action: 1}}
	plan, err := PlanExceptionBridge(cie, fde, lsda, mapped, []NativeCallLocation{{OriginalPC: 0x1001, VMOffset: 0x24}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Thunks) != 1 || plan.Thunks[0].ID == 0 || plan.Personality != personality || plan.Thunks[0].VMLandingPad != 0x80 {
		t.Fatalf("plan=%+v", plan)
	}
	encoded, err := BuildBridgeLSDA(plan, plan.Thunks[0], InvokeThunkLayout{CallOffset: 12, CallLength: 4, LandingOffset: 32, RangeLength: 64})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded.Relocations) != 1 || encoded.Relocations[0].Target != typeAddress || encoded.Relocations[0].Encoding != PEPcrel|PESdata4 {
		t.Fatalf("relocations=%+v", encoded.Relocations)
	}
	materialized, err := encoded.ApplyRelocations(0x7000)
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := ParseLSDA(materialized, 0x7000, 0x6000, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(reparsed.CallSites) != 1 || reparsed.CallSites[0].Start != 0x600c || reparsed.CallSites[0].Length != 4 || reparsed.CallSites[0].LandingPad != 0x6020 || reparsed.CallSites[0].Action != 1 {
		t.Fatalf("rebuilt call sites=%+v", reparsed.CallSites)
	}
	if reparsed.TypeInfos[1].Address != typeAddress {
		t.Fatalf("relocated type info=%+v", reparsed.TypeInfos[1])
	}
}

func TestExceptionBridgePlanRejectsAmbiguousOrIncompleteMetadata(t *testing.T) {
	personality := uint64(1)
	lsdaAddress := uint64(2)
	cie := &CIE{Personality: &personality, PersonalityEncoding: PEPcrel | PESdata4}
	fde := &FDE{LSDA: &lsdaAddress}
	lsda := &LSDA{
		CallSites:    []CallSite{{Start: 0x1000, Length: 8, LandingPad: 0x1010, Action: 1}},
		ActionChains: map[uint64][]ActionRecord{},
	}
	if _, err := PlanExceptionBridge(cie, fde, lsda, []MappedCallSite{{VMLandingPad: 4}}, []NativeCallLocation{{OriginalPC: 0x1000}}); err == nil {
		t.Fatal("missing action chain was accepted")
	}
	lsda.ActionChains[1] = []ActionRecord{{Offset: 1, TypeFilter: 1}}
	if _, err := PlanExceptionBridge(cie, fde, lsda, []MappedCallSite{{VMLandingPad: 0}}, []NativeCallLocation{{OriginalPC: 0x1000}}); err == nil {
		t.Fatal("missing landing mapping was accepted")
	}
}

func TestParseEHFrameHeaderSearchTable(t *testing.T) {
	const address = uint64(0x4000)
	data := []byte{1, PEPcrel | PESdata4, PEUdata4, PEDatarel | PESdata4}
	append32 := func(value int32) { data = binary.LittleEndian.AppendUint32(data, uint32(value)) }
	append32(int32(0x4100 - (address + 4)))
	append32(2)
	append32(0x100)
	append32(0x200)
	append32(0x180)
	append32(0x220)
	header, err := ParseEHFrameHeader(data, address, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	if header.EHFrameAddress != 0x4100 || len(header.Entries) != 2 || header.Entries[0].InitialLocation != 0x4100 || header.Entries[1].FDEAddress != 0x4220 {
		t.Fatalf("header=%+v", header)
	}
	data[len(data)-8] = 0
	if _, err := ParseEHFrameHeader(data, address, binary.LittleEndian, 8); err == nil {
		t.Fatal("unordered search table accepted")
	}
}

func TestBuildEHFrameHeaderCanonicalRoundTrip(t *testing.T) {
	const address = uint64(0x8000)
	entries := []HeaderEntry{
		{InitialLocation: 0xa000, FDEAddress: 0x8300},
		{InitialLocation: 0x9000, FDEAddress: 0x8200},
	}
	before := append([]HeaderEntry(nil), entries...)
	data, err := BuildEHFrameHeader(address, 0x7000, entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 12+len(entries)*8 || data[0] != 1 || data[1] != PEPcrel|PESdata4 || data[2] != PEUdata4 || data[3] != PEDatarel|PESdata4 {
		t.Fatalf("header bytes=%x", data)
	}
	parsed, err := ParseEHFrameHeader(data, address, binary.LittleEndian, 8)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.EHFrameAddress != 0x7000 || len(parsed.Entries) != 2 ||
		parsed.Entries[0].InitialLocation != 0x9000 || parsed.Entries[0].FDEAddress != 0x8200 ||
		parsed.Entries[1].InitialLocation != 0xa000 || parsed.Entries[1].FDEAddress != 0x8300 {
		t.Fatalf("parsed=%+v", parsed)
	}
	for i := range entries {
		if entries[i] != before[i] {
			t.Fatal("BuildEHFrameHeader mutated caller entries")
		}
	}
}

func TestBuildEHFrameHeaderRejectsDuplicateAndOutOfRangeEntries(t *testing.T) {
	if _, err := BuildEHFrameHeader(0x8000, 0x7000, []HeaderEntry{
		{InitialLocation: 0x9000, FDEAddress: 0x8200},
		{InitialLocation: 0x9000, FDEAddress: 0x8300},
	}); err == nil {
		t.Fatal("duplicate initial location was accepted")
	}
	if _, err := BuildEHFrameHeader(0x1000, 0x100000000, []HeaderEntry{{InitialLocation: 0x2000, FDEAddress: 0x3000}}); err == nil {
		t.Fatal("out-of-range .eh_frame displacement was accepted")
	}
	if _, err := BuildEHFrameHeader(0x1000, 0x2000, []HeaderEntry{{InitialLocation: 0x100000000, FDEAddress: 0x3000}}); err == nil {
		t.Fatal("out-of-range table displacement was accepted")
	}
}

func TestUnwindParsersFailClosedOnMalformedInput(t *testing.T) {
	if _, err := ParseEHFrame([]byte{8, 0, 0, 0, 0}, 0, binary.LittleEndian, 8); err == nil {
		t.Fatal("truncated .eh_frame accepted")
	}
	if _, err := ParseLSDA([]byte{PEOmit, PEOmit, PEUleb128, 0x80}, 0, 0, binary.LittleEndian, 8); err == nil {
		t.Fatal("truncated LSDA accepted")
	}
}

func appendLength(dst, body []byte) []byte {
	length := make([]byte, 4)
	binary.LittleEndian.PutUint32(length, uint32(len(body)))
	dst = append(dst, length...)
	return append(dst, body...)
}
