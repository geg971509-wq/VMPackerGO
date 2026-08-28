package vm

import "testing"

func TestExclusiveRegionIDIsContentAddressed(t *testing.T) {
	words := []uint32{0xc85ffc20, 0x91000421, 0xc802fc21}
	a := NewExclusiveRegion(words)
	b := NewExclusiveRegion(append([]uint32(nil), words...))
	if !a.Valid() || a.ID != b.ID || a.ID == 0 {
		t.Fatalf("invalid deterministic region IDs: %#v %#v", a, b)
	}
	b.Instructions[1] ^= 1
	if b.Valid() || NewExclusiveRegion(b.Instructions).ID == a.ID {
		t.Fatal("mutated region retained its content identity")
	}
}
