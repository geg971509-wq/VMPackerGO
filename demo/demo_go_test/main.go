package main

/*
#include <stdint.h>
*/
import "C"

// check_key is intentionally exported through cgo so the Android c-shared
// fixture has an explicit AAPCS64 boundary. VMPacker must never guess Go's
// internal ABI for ordinary Go functions.
//
//export check_key
func check_key(input C.uint64_t) C.uint64_t {
	return C.uint64_t(((uint64(input) * 7) + 42) ^ 0xFF)
}

func main() {}
