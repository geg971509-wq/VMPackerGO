package arm64

import "github.com/geg971509-wq/VMPackerGO/internal/vm"

const (
	caspMask  uint32 = 0xBFA07C00
	caspValue uint32 = 0x08207C00
)

// lseAtomicPatterns keeps FEAT_LSE atomics explicit rather than widening the
// generic load/store table into an unreviewed catch-all. CASP is modeled as
// the CAS semantic family with a pair-specific raw encoding; the translator
// uses that encoding to select the pair transport kind while preserving the
// existing seven-byte OpAtomic wire format.
var lseAtomicPatterns = []InstrPattern{
	{
		Name: "CASP", Mask: caspMask, Value: caspValue, Op: CAS,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postCasp,
	},
	{
		Name: "SWP", Mask: 0x3F20FC00, Value: 0x38208000, Op: SWP,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDCLR", Mask: 0x3F20FC00, Value: 0x38201000, Op: LDCLR,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDEOR", Mask: 0x3F20FC00, Value: 0x38202000, Op: LDEOR,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDSET", Mask: 0x3F20FC00, Value: 0x38203000, Op: LDSET,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDSMAX", Mask: 0x3F20FC00, Value: 0x38204000, Op: LDSMAX,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDSMIN", Mask: 0x3F20FC00, Value: 0x38205000, Op: LDSMIN,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDUMAX", Mask: 0x3F20FC00, Value: 0x38206000, Op: LDUMAX,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
	{
		Name: "LDUMIN", Mask: 0x3F20FC00, Value: 0x38207000, Op: LDUMIN,
		Fields: []FieldDef{{Name: "size", Hi: 31, Lo: 30}, fRm16, fRn, fRd},
		Post:   postLdadd,
	},
}

func isCASPPair(inst vm.Instruction) bool {
	return Op(inst.Op) == CAS && inst.Raw&caspMask == caspValue
}

func postCasp(f map[string]int64, inst *vm.Instruction) {
	sz := f["size"]
	// CASP has two architectural pair widths. With bit31 fixed to zero by the
	// pattern, size=00 is Wt/Wt2 (4-byte members) and size=01 is Xt/Xt2
	// (8-byte members). The high member is implicit low+1 in both register
	// pairs and is validated separately by the product policy.
	inst.Shift = 4 << int(sz)
	inst.SF = sz == 1
}

func isLoadReturnLSE(op Op) bool {
	switch op {
	case LDADD, SWP, LDCLR, LDEOR, LDSET, LDSMAX, LDSMIN, LDUMAX, LDUMIN:
		return true
	default:
		return false
	}
}

func atomicUsesRm(op Op) bool {
	return isLoadReturnLSE(op) || op == CAS
}
