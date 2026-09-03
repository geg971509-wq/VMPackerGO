package arm64

// lseAtomicPatterns covers the single-register FEAT_LSE read-modify-write
// family that shares LDADD's width/register/order encoding. Keep these
// separate from the general load/store table so the product surface is
// explicit and the generic table does not become an unreviewed LSE catch-all.
var lseAtomicPatterns = []InstrPattern{
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
