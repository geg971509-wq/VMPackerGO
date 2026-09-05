// This fixture intentionally has one exported, position-independent arithmetic
// function. The iOS validation script compiles it with Apple's arm64 clang and
// then sends the resulting MH_DYLIB through the CLI.
__attribute__((visibility("default"), noinline))
int vmp_fixture_add(int a, int b) {
    return a + b;
}
