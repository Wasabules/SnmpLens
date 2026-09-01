package mib

import (
	"os"
	"path/filepath"
	"testing"
)

func bigMIB(b *testing.B) string {
	b.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "mibs", "IP-MIB"))
	if err != nil {
		b.Skip("no corpus")
	}
	content, _ := NormaliseSource(raw)
	return content
}

func benchCatalogue() Catalogue {
	// A realistic loaded tree: the bundled MIBs export a few thousand symbols.
	cat := Catalogue{Modules: make([]string, 0, 14)}
	for i := 0; i < 14; i++ {
		cat.Modules = append(cat.Modules, string(rune('A'+i))+"-MIB")
	}
	for i := 0; i < 3000; i++ {
		cat.Symbols = append(cat.Symbols, Symbol{
			Name:   "sym" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('0'+i%10)),
			Module: cat.Modules[i%14],
			Kind:   "node",
		})
	}
	return cat
}

func BenchmarkValidate(b *testing.B) {
	src := bigMIB(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Validate(src)
	}
}

func BenchmarkCheckImports(b *testing.B) {
	src := bigMIB(b)
	cat := benchCatalogue()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CheckImports(src, cat)
	}
}

func BenchmarkAnalyse(b *testing.B) {
	src := bigMIB(b)
	cat := benchCatalogue()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Analyse(src, cat)
	}
}

// What the editor actually does on every pause in typing.
func BenchmarkEditorRoundTrip(b *testing.B) {
	src := bigMIB(b)
	cat := benchCatalogue()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Validate(src)
		CheckImports(src, cat)
		Analyse(src, cat)
	}
}

func BenchmarkScanIdentifiers(b *testing.B) {
	src := bigMIB(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanIdentifiers(src)
	}
}

// What the editor now does on every pause in typing.
func BenchmarkAnalyseAll(b *testing.B) {
	src := bigMIB(b)
	cat := benchCatalogue()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AnalyseAll(src, cat)
	}
}
