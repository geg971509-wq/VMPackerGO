package abi

import (
	"fmt"
	"strings"
)

const MaxParams = 8

type Type string

const (
	I8  Type = "i8"
	U8  Type = "u8"
	I16 Type = "i16"
	U16 Type = "u16"
	I32 Type = "i32"
	U32 Type = "u32"
	I64 Type = "i64"
	U64 Type = "u64"
	Ptr Type = "ptr"
)

type Signature struct {
	Params []Type `json:"params"`
	Result Type   `json:"result,omitempty"`
	Void   bool   `json:"-"`
}

func Parse(text string) (Signature, error) {
	text = strings.TrimSpace(text)
	open := strings.IndexByte(text, '(')
	if open <= 0 || !strings.HasSuffix(text, ")") || strings.Contains(text[open+1:len(text)-1], "(") {
		return Signature{}, fmt.Errorf("invalid ABI %q; expected result(param,...), for example i32(ptr,u64)", text)
	}

	var sig Signature
	result := strings.TrimSpace(text[:open])
	if result == "void" {
		sig.Void = true
	} else {
		t, err := parseType(result)
		if err != nil {
			return Signature{}, fmt.Errorf("invalid ABI result: %w", err)
		}
		sig.Result = t
	}

	params := strings.TrimSpace(text[open+1 : len(text)-1])
	if params == "" {
		sig.Params = []Type{}
		return sig, nil
	}
	parts := strings.Split(params, ",")
	if len(parts) > MaxParams {
		return Signature{}, fmt.Errorf("ABI has %d parameters; maximum is %d", len(parts), MaxParams)
	}
	for _, part := range parts {
		t, err := parseType(strings.TrimSpace(part))
		if err != nil {
			return Signature{}, fmt.Errorf("invalid ABI parameter: %w", err)
		}
		sig.Params = append(sig.Params, t)
	}
	return sig, nil
}

func FromParts(params []string, result string) (Signature, error) {
	if len(params) > MaxParams {
		return Signature{}, fmt.Errorf("ABI has %d parameters; maximum is %d", len(params), MaxParams)
	}
	sig := Signature{Params: make([]Type, 0, len(params))}
	for _, param := range params {
		t, err := parseType(param)
		if err != nil {
			return Signature{}, fmt.Errorf("invalid ABI parameter: %w", err)
		}
		sig.Params = append(sig.Params, t)
	}
	if result == "void" {
		sig.Void = true
		return sig, nil
	}
	t, err := parseType(result)
	if err != nil {
		return Signature{}, fmt.Errorf("invalid ABI result: %w", err)
	}
	sig.Result = t
	return sig, nil
}

func (s Signature) ResultName() string {
	if s.Void {
		return "void"
	}
	return string(s.Result)
}

func (s Signature) String() string {
	params := make([]string, len(s.Params))
	for i, param := range s.Params {
		params[i] = string(param)
	}
	return s.ResultName() + "(" + strings.Join(params, ",") + ")"
}

func parseType(text string) (Type, error) {
	t := Type(text)
	switch t {
	case I8, U8, I16, U16, I32, U32, I64, U64, Ptr:
		return t, nil
	default:
		return "", fmt.Errorf("unsupported type %q", text)
	}
}
