from pathlib import Path

p = Path('internal/arch/arm64/compiler_corpus_test.go')
text = p.read_text()
old = '''func configureCompilerOutlinedTailInlines(translator *Translator, key compilerCorpusKey, group []compilerCorpusRecord, groups map[compilerCorpusKey][]compilerCorpusRecord) error {'''
new = '''func isCompilerOutlinedCorpusName(name string) bool {
	const prefix = "OUTLINED_FUNCTION_"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(name, prefix)
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func configureCompilerOutlinedTailInlines(translator *Translator, key compilerCorpusKey, group []compilerCorpusRecord, groups map[compilerCorpusKey][]compilerCorpusRecord) error {'''
if text.count(old) != 1:
    raise SystemExit('configure marker mismatch')
text = text.replace(old, new, 1)
old = '!strings.HasPrefix(candidateKey.Function, "OUTLINED_FUNCTION_") || len(candidate) == 0 || candidate[0].Address != target {'
new = '!isCompilerOutlinedCorpusName(candidateKey.Function) || len(candidate) == 0 || candidate[0].Address != target {'
if text.count(old) != 1:
    raise SystemExit('helper name predicate mismatch')
text = text.replace(old, new, 1)
old = '''		// Side metadata and source-map closure are product obligations only for
		// functions that actually translated. Intentional fail-closed functions
		// are rejected before any partial metadata can be consumed.'''
new = '''		// Side metadata and source-map closure are product obligations only for
		// functions that actually translated. Any unsupported record above is a
		// hard unexpected compiler gap; there are no intentional exemptions.'''
if text.count(old) != 1:
    raise SystemExit('stale comment mismatch')
text = text.replace(old, new, 1)
p.write_text(text)
