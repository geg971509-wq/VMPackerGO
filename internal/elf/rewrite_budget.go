package elf

import "fmt"

const (
	// Existing public limits allow at most 1 GiB of input and 4096 functions
	// with 256 KiB final bytecode each. Bound all newly appended load data to
	// that same 1 GiB order of magnitude, and bound the final file endpoint to
	// 2 GiB. This prevents a valid-looking request from multiplying host memory
	// without inventing a smaller per-function limit.
	maxRewriteExpansion = uint64(1 << 30)
	maxRewriteOutput    = uint64(2 << 30)
)

func validateRewriteBudget(inputSize uint64, segments []rewriteSegment) error {
	if inputSize > uint64(1<<30) {
		return fmt.Errorf("input size %d exceeds the 1 GiB product limit", inputSize)
	}
	var expansion uint64
	finalEnd := inputSize
	for index, segment := range segments {
		size := uint64(len(segment.data))
		if segment.fileSize != 0 && segment.fileSize != size {
			return fmt.Errorf("planned segment %d file size %d does not match data size %d", index, segment.fileSize, size)
		}
		var ok bool
		expansion, ok = checkedAdd(expansion, size)
		if !ok || expansion > maxRewriteExpansion {
			return fmt.Errorf("aggregate rewrite expansion exceeds the 1 GiB product budget")
		}
		end, ok := checkedAdd(segment.fileOffset, size)
		if !ok {
			return fmt.Errorf("planned segment %d file endpoint overflows", index)
		}
		if end > finalEnd {
			finalEnd = end
		}
	}
	if finalEnd > maxRewriteOutput {
		return fmt.Errorf("final rewrite file endpoint 0x%x exceeds the 2 GiB product budget", finalEnd)
	}
	return nil
}
