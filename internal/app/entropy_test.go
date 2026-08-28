package app

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/vmpacker/internal/vm"
)

func TestSeededEntropyIsLocalAndDeterministic(t *testing.T) {
	seed := strings.Repeat("01", 32)
	first, err := runEntropy(seed)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runEntropy(seed)
	if err != nil {
		t.Fatal(err)
	}
	other, err := runEntropy(strings.Repeat("02", 32))
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 1024)
	b := make([]byte, 1024)
	c := make([]byte, 1024)
	if _, err := io.ReadFull(first, a); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(second, b); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(other, c); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("same seed produced different entropy")
	}
	if bytes.Equal(a, c) {
		t.Fatal("different seeds produced identical entropy")
	}

	mapReaderA, _ := runEntropy(seed)
	mapReaderB, _ := runEntropy(seed)
	mapA, err := vm.NewOpcodeMap(mapReaderA)
	if err != nil {
		t.Fatal(err)
	}
	mapB, err := vm.NewOpcodeMap(mapReaderB)
	if err != nil {
		t.Fatal(err)
	}
	digestA, _ := mapA.Digest()
	digestB, _ := mapB.Digest()
	if digestA != digestB {
		t.Fatal("same seed produced different opcode maps")
	}
}
