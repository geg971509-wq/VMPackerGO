package vm

// REG_XZR is the marker used for the AArch64 zero register.
const REG_XZR = -2

// Instruction is the decoded instruction representation shared by the active
// AArch64 decoder and translator.
type Instruction struct {
	Raw       uint32
	Op        int
	Rd        int
	Rn        int
	Rm        int
	Imm       int64
	Shift     int
	ShiftType int
	Cond      int
	SF        bool
	Offset    int
	WB        int
}

// FuncInfo identifies a function in an ELF input.
type FuncInfo struct {
	Name    string
	Addr    uint64
	Size    uint64
	Offset  uint64
	Section string
}
