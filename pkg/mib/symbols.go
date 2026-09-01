package mib

import (
	"sort"
	"strings"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/parser"
)

// The symbol catalogue and the missing-import check.
//
// This is the assistance that answers the question people actually have in
// front of a vendor MIB: what is the exact name, and which module does it come
// from. The data already exists — it is the loaded tree — so the only work is
// exposing it and comparing it against what the buffer imports.

// Symbol is one name a MIB can refer to.
type Symbol struct {
	Name   string `json:"name"`
	Module string `json:"module"`
	// Kind is "node", "type" or "module".
	Kind        string `json:"kind"`
	Oid         string `json:"oid,omitempty"`
	Description string `json:"description,omitempty"`
}

// Catalogue is everything the loaded tree knows, for the editor's picker.
type Catalogue struct {
	Modules []string `json:"modules"`
	Symbols []Symbol `json:"symbols"`
}

// Symbols lists every name in the loaded tree.
//
// Read-locked: the editor asks for this while other tabs resolve OIDs, and a
// reload can be rebuilding the world at the same time.
func Symbols() Catalogue {
	gosmiMu.RLock()
	defer gosmiMu.RUnlock()

	cat := Catalogue{Modules: []string{}, Symbols: []Symbol{}}
	seen := map[string]bool{}

	for _, m := range gosmi.GetLoadedModules() {
		name := m.Name
		if name == "" {
			continue
		}
		cat.Modules = append(cat.Modules, name)

		for _, n := range m.GetNodes() {
			key := name + "." + n.Name
			if n.Name == "" || seen[key] {
				continue
			}
			seen[key] = true
			cat.Symbols = append(cat.Symbols, Symbol{
				Name: n.Name, Module: name, Kind: "node",
				Oid:         n.Oid.String(),
				Description: firstLine(n.Description),
			})
		}
		for _, t := range m.GetTypes() {
			key := name + "." + t.Name
			if t.Name == "" || seen[key] {
				continue
			}
			seen[key] = true
			cat.Symbols = append(cat.Symbols, Symbol{
				Name: t.Name, Module: name, Kind: "type",
				Description: firstLine(t.Description),
			})
		}
	}

	sort.Strings(cat.Modules)
	sort.Slice(cat.Symbols, func(i, j int) bool { return cat.Symbols[i].Name < cat.Symbols[j].Name })
	return cat
}

// firstLine keeps a description short enough for a list row.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// baseTypes are built into SMI and never imported.
var baseTypes = map[string]bool{
	"INTEGER": true, "OCTET": true, "STRING": true, "OBJECT": true,
	"IDENTIFIER": true, "NULL": true, "SEQUENCE": true, "OF": true,
	"CHOICE": true, "BITS": true, "SIZE": true, "IMPLIED": true,
	"DEFINITIONS": true, "BEGIN": true, "END": true, "IMPORTS": true,
	"EXPORTS": true, "FROM": true, "MAX-ACCESS": true, "STATUS": true,
	"DESCRIPTION": true, "SYNTAX": true, "INDEX": true, "AUGMENTS": true,
	"UNITS": true, "REFERENCE": true, "DEFVAL": true, "ACCESS": true,
	"MIN-ACCESS": true, "OBJECTS": true, "NOTIFICATIONS": true, "MODULE": true,
	"MANDATORY-GROUPS": true, "GROUP": true, "WRITE-SYNTAX": true,
	"LAST-UPDATED": true, "ORGANIZATION": true, "CONTACT-INFO": true,
	"REVISION": true, "VARIABLES": true, "SUPPORTS": true, "PRODUCT-RELEASE": true,
	"MODULE-IDENTITY": true, "OBJECT-TYPE": true, "OBJECT-IDENTITY": true,
	"NOTIFICATION-TYPE": true, "TEXTUAL-CONVENTION": true, "OBJECT-GROUP": true,
	"NOTIFICATION-GROUP": true, "MODULE-COMPLIANCE": true, "AGENT-CAPABILITIES": true,
	"TRAP-TYPE": true, "MACRO": true,
}

// MissingImport is a symbol the buffer uses but never imports.
type MissingImport struct {
	Symbol string `json:"symbol"`
	// Module is where it is defined in the loaded tree.
	Module string `json:"module"`
	// Line and Column locate the first use.
	Line   int `json:"line"`
	Column int `json:"column"`
}

// ImportFix describes how to repair a set of missing imports.
type ImportFix struct {
	Missing []MissingImport `json:"missing"`
	// Content is the whole buffer with the IMPORTS clause repaired, ready to
	// replace what the editor holds. Empty when there is nothing to fix.
	Content string `json:"content,omitempty"`
}

// scanIdentifiers walks MIB source and returns every identifier used outside a
// comment or a string, with the position of its first appearance.
//
// Comments and strings have to be skipped for this to be usable at all: a
// DESCRIPTION is prose, and prose is full of words that happen to match symbol
// names.
func scanIdentifiers(content string) map[string][2]int {
	found := map[string][2]int{}
	inString := false

	for lineNo, line := range strings.Split(content, "\n") {
		i := 0
		for i < len(line) {
			if inString {
				if end := strings.IndexByte(line[i:], '"'); end >= 0 {
					i += end + 1
					inString = false
					continue
				}
				break
			}
			switch {
			case line[i] == '"':
				inString = true
				i++
			case line[i] == '-' && i+1 < len(line) && line[i+1] == '-':
				if close := strings.Index(line[i+2:], "--"); close >= 0 {
					i += 2 + close + 2
				} else {
					i = len(line)
				}
			case isIdentStart(line[i]):
				j := i
				for j < len(line) && isIdentPart(line[j]) {
					j++
				}
				word := line[i:j]
				if _, seen := found[word]; !seen {
					found[word] = [2]int{lineNo + 1, i + 1}
				}
				i = j
			default:
				i++
			}
		}
	}
	return found
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// CheckImports finds symbols the buffer uses without importing them.
//
// Only names that the loaded tree actually knows are reported. That keeps the
// false-positive rate near zero: a stray English word in a DESCRIPTION will not
// match a loaded symbol, and an unknown vendor name is not something we could
// suggest an import for anyway.
func CheckImports(content string, cat Catalogue) []MissingImport {
	module, err := parser.Parse(strings.NewReader(content))
	if err != nil || module == nil {
		return nil // a file that does not parse has bigger problems
	}

	// What the buffer already has: imported names, plus everything it defines
	// itself.
	known := map[string]bool{}
	for _, imp := range module.Body.Imports {
		for _, n := range imp.Names {
			known[string(n)] = true
		}
	}
	if module.Body.Identity != nil {
		known[string(module.Body.Identity.Name)] = true
	}
	for _, n := range module.Body.Nodes {
		known[string(n.Name)] = true
	}
	for _, t := range module.Body.Types {
		known[string(t.Name)] = true
	}
	for _, m := range module.Body.Macros {
		known[string(m.Name)] = true
	}

	// Where each catalogue symbol lives. A name defined in several modules
	// resolves to the first alphabetically, which is stable rather than
	// arbitrary.
	origin := map[string]string{}
	for _, s := range cat.Symbols {
		if _, exists := origin[s.Name]; !exists {
			origin[s.Name] = s.Module
		}
	}

	selfName := string(module.Name)
	var missing []MissingImport
	for word, pos := range scanIdentifiers(content) {
		if known[word] || baseTypes[word] || word == selfName {
			continue
		}
		mod, ok := origin[word]
		if !ok || mod == selfName {
			continue
		}
		missing = append(missing, MissingImport{
			Symbol: word, Module: mod, Line: pos[0], Column: pos[1],
		})
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Line != missing[j].Line {
			return missing[i].Line < missing[j].Line
		}
		return missing[i].Symbol < missing[j].Symbol
	})
	return missing
}

// FixImports returns the buffer with the missing symbols added to IMPORTS.
//
// It edits the text rather than regenerating it from the AST: a MIB carries
// comments, alignment and copyright headers that no printer would preserve,
// and an assistant that reformats your file while fixing one line is not an
// assistant.
func FixImports(content string, cat Catalogue) ImportFix {
	missing := CheckImports(content, cat)
	if len(missing) == 0 {
		return ImportFix{Missing: []MissingImport{}}
	}

	byModule := map[string][]string{}
	for _, m := range missing {
		byModule[m.Module] = append(byModule[m.Module], m.Symbol)
	}
	modules := make([]string, 0, len(byModule))
	for m := range byModule {
		sort.Strings(byModule[m])
		modules = append(modules, m)
	}
	sort.Strings(modules)

	lines := strings.Split(content, "\n")
	fixed := insertImports(lines, modules, byModule)
	return ImportFix{Missing: missing, Content: strings.Join(fixed, "\n")}
}

// insertImports adds clauses to an existing IMPORTS block, or creates one just
// after BEGIN when there is none.
func insertImports(lines, modules []string, byModule map[string][]string) []string {
	clauses := make([]string, 0, len(modules)*2)
	for _, m := range modules {
		clauses = append(clauses,
			"    "+strings.Join(byModule[m], ", "),
			"        FROM "+m)
	}

	importsAt, semicolonAt, beginAt := -1, -1, -1
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if importsAt < 0 && trimmed == "IMPORTS" {
			importsAt = i
		}
		if beginAt < 0 && strings.Contains(l, "BEGIN") {
			beginAt = i
		}
		if importsAt >= 0 && i >= importsAt && strings.HasSuffix(trimmed, ";") {
			semicolonAt = i
			break
		}
	}

	if importsAt >= 0 && semicolonAt >= 0 {
		// Append before the terminating semicolon, which moves to its own
		// clause so the existing last line keeps its shape.
		last := lines[semicolonAt]
		lines[semicolonAt] = strings.TrimSuffix(strings.TrimRight(last, " \t"), ";")

		out := append([]string{}, lines[:semicolonAt+1]...)
		out = append(out, clauses...)
		out[len(out)-1] += ";"
		return append(out, lines[semicolonAt+1:]...)
	}

	if beginAt >= 0 {
		block := append([]string{"", "IMPORTS"}, clauses...)
		block[len(block)-1] += ";"
		out := append([]string{}, lines[:beginAt+1]...)
		out = append(out, block...)
		return append(out, lines[beginAt+1:]...)
	}

	return lines
}
