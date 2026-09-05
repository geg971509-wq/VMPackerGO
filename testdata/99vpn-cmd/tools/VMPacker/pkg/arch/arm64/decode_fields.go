package arm64

import "github.com/vmpacker/pkg/vm"

// ============================================================
// ARM64 表驱动解码引擎
//
// FieldDef  — 描述一个位域的 Hi/Lo 位置，消除手工移位歧义
// InstrPattern — Mask/Value 匹配 + Fields 自动提取 + Post 回调
// ============================================================

// FieldDef 位域定义
type FieldDef struct {
	Name   string // 字段名: "sf", "Rd", "Rn", "imm7" ...
	Hi     int    // 高位（含），例如 bit31 → Hi=31
	Lo     int    // 低位（含），例如 bit0  → Lo=0
	Signed bool   // 是否有符号扩展
}

// PostFunc 后处理回调：处理表无法表达的逻辑（XZR替换、offset缩放等）
type PostFunc func(fields map[string]int64, inst *vm.Instruction)

// InstrPattern 指令模式定义
type InstrPattern struct {
	Name   string     // 调试用名称，如 "ADD_IMM"
	Mask   uint32     // 固定位掩码
	Value  uint32     // 固定位期望值
	Op     Op         // 解码后的指令类型
	Fields []FieldDef // 位域定义列表
	Post   PostFunc   // 可选后处理
}

// ---- 位域提取 ----

// extractField 从 raw 中提取单个位域
func extractField(raw uint32, f FieldDef) int64 {
	width := f.Hi - f.Lo + 1
	mask := uint32((1 << width) - 1)
	val := (raw >> uint(f.Lo)) & mask
	if f.Signed {
		return SignExtend(val, width)
	}
	return int64(val)
}

// extractFields 从 raw 中提取所有位域，返回 name→value 映射
func extractFields(raw uint32, fields []FieldDef) map[string]int64 {
	result := make(map[string]int64, len(fields))
	for _, f := range fields {
		result[f.Name] = extractField(raw, f)
	}
	return result
}

// ---- 通用字段映射 ----

// applyCommonFields 将常见字段名映射到 vm.Instruction
//
// 约定: Rd→inst.Rd, Rn→inst.Rn, Rm→inst.Rm, sf→inst.SF,
//       cond→inst.Cond, wb→inst.WB, shift→inst.Shift
//
// inst.Imm 由各指令的 Post 回调设置（因为 imm 宽度/缩放各不相同）
func applyCommonFields(fields map[string]int64, inst *vm.Instruction) {
	if v, ok := fields["Rd"]; ok {
		inst.Rd = int(v)
	}
	if v, ok := fields["Rn"]; ok {
		inst.Rn = int(v)
	}
	if v, ok := fields["Rm"]; ok {
		inst.Rm = int(v)
	}
	if v, ok := fields["sf"]; ok {
		inst.SF = v != 0
	}
	if v, ok := fields["cond"]; ok {
		inst.Cond = int(v)
	}
	if v, ok := fields["wb"]; ok {
		inst.WB = int(v)
	}
	if v, ok := fields["shift"]; ok {
		inst.Shift = int(v)
	}
}

// ---- 模式匹配 ----

// matchAndDecode 在 patterns 中查找第一个匹配的模式，解码并填充 inst
// 返回是否匹配成功
func matchAndDecode(raw uint32, patterns []InstrPattern, inst *vm.Instruction) bool {
	for i := range patterns {
		p := &patterns[i]
		if raw&p.Mask == p.Value {
			inst.Op = int(p.Op)
			fields := extractFields(raw, p.Fields)
			applyCommonFields(fields, inst)
			if p.Post != nil {
				p.Post(fields, inst)
			}
			return true
		}
	}
	return false
}

// ---- 辅助函数 ----

// xzrReplace ARM64 XZR 标记: reg==31 → REG_XZR
func xzrReplace(reg *int) {
	if *reg == 31 {
		*reg = vm.REG_XZR
	}
}
