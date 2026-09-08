package mib

import (
	"fmt"
	"strings"

	"github.com/sleepinggenius2/gosmi/parser"
)

// The outline: what this file DEFINES, in the order it defines it.
//
// The question in front of somebody editing a vendor MIB is "where is
// ifXEntry", and the answer is four hundred lines further down. A MIB has no
// indentation to navigate by and no folding to collapse — every definition
// starts hard against the left margin — so scrolling is the only tool, and a
// 185 KB file is a lot of scrolling.
//
// It costs NOTHING. AnalyseAll already parses the buffer once, 350 ms after
// every pause in typing, and every field below comes off that same parse — no
// second pass, no gosmi, and in particular no gosmi LOCK. An outline built from
// the loaded tree instead would describe the file as it was when it was last
// loaded, which for a file being edited is the one description that is wrong.

// Outline kinds. Not the SMI's own vocabulary but the reader's: what the thing
// IS in the tree, which is what somebody scanning a list wants to tell apart.
const (
	OutlineModule       = "module"
	OutlineObject       = "object"
	OutlineTable        = "table"
	OutlineRow          = "row"
	OutlineIdentity     = "identity"
	OutlineNotification = "notification"
	OutlineGroup        = "group"
	OutlineCompliance   = "compliance"
	OutlineCapabilities = "capabilities"
	OutlineTrap         = "trap"
	OutlineNode         = "node"
	OutlineType         = "type"
	OutlineMacro        = "macro"
)

// OutlineItem is one definition, with where it is and what it says about
// itself.
type OutlineItem struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Line and Column are 1-based, like every position in this package.
	Line   int `json:"line"`
	Column int `json:"column"`
	// Parent is the name this definition is placed under — the first
	// sub-identifier of `::= { parent n }` — and SubID the number after it.
	// Together they are what makes an outline a TREE rather than a list.
	Parent string `json:"parent,omitempty"`
	SubID  string `json:"subId,omitempty"`
	// Syntax, Access and Status are the three things a reader checks before
	// scrolling to a definition, so the list carries them instead of making the
	// scroll the way to find out.
	Syntax string `json:"syntax,omitempty"`
	Access string `json:"access,omitempty"`
	Status string `json:"status,omitempty"`
	// Description is the first line only. The whole DESCRIPTION of a single
	// object routinely runs longer than this entire structure.
	Description string `json:"description,omitempty"`
}

// outlineOf reads the definitions out of an already-parsed module.
//
// A parse that FAILED still has a module for everything it managed to read, so
// the outline of a file with a syntax error on line 412 still lists what comes
// before it. That is the moment an outline is most useful and the easiest one
// to drop by accident.
func outlineOf(module *parser.Module) []OutlineItem {
	out := []OutlineItem{}
	if module == nil {
		return out
	}

	if id := module.Body.Identity; id != nil {
		out = append(out, OutlineItem{
			Name: string(id.Name), Kind: OutlineModule,
			Line: id.Pos.Line, Column: id.Pos.Column,
			Description: firstLine(id.Description),
		})
	}

	for i := range module.Body.Types {
		t := &module.Body.Types[i]
		item := OutlineItem{
			Name: string(t.Name), Kind: OutlineType,
			Line: t.Pos.Line, Column: t.Pos.Column,
		}
		if tc := t.TextualConvention; tc != nil {
			item.Syntax = syntaxTypeName(&tc.Syntax)
			item.Status = string(tc.Status)
			item.Description = firstLine(tc.Description)
		} else if t.Sequence != nil {
			item.Syntax = "SEQUENCE"
		}
		out = append(out, item)
	}

	for i := range module.Body.Nodes {
		n := &module.Body.Nodes[i]
		item := OutlineItem{
			Name: string(n.Name), Kind: OutlineNode,
			Line: n.Pos.Line, Column: n.Pos.Column,
		}

		switch {
		case n.ObjectType != nil:
			ot := n.ObjectType
			item.Kind = OutlineObject
			if ot.Syntax.Sequence != nil {
				item.Kind = OutlineTable
			} else if len(ot.Index) > 0 || ot.Augments != nil {
				item.Kind = OutlineRow
			}
			item.Syntax = syntaxName(&ot.Syntax)
			item.Access = string(ot.Access)
			item.Status = string(ot.Status)
			item.Description = firstLine(ot.Description)
		case n.ObjectIdentity != nil:
			item.Kind = OutlineIdentity
			item.Status = string(n.ObjectIdentity.Status)
			item.Description = firstLine(n.ObjectIdentity.Description)
		case n.NotificationType != nil:
			item.Kind = OutlineNotification
			item.Status = string(n.NotificationType.Status)
			item.Description = firstLine(n.NotificationType.Description)
		case n.ObjectGroup != nil:
			item.Kind = OutlineGroup
			item.Status = string(n.ObjectGroup.Status)
			item.Description = firstLine(n.ObjectGroup.Description)
		case n.NotificationGroup != nil:
			item.Kind = OutlineGroup
			item.Status = string(n.NotificationGroup.Status)
			item.Description = firstLine(n.NotificationGroup.Description)
		case n.ModuleCompliance != nil:
			item.Kind = OutlineCompliance
			item.Status = string(n.ModuleCompliance.Status)
			item.Description = firstLine(n.ModuleCompliance.Description)
		case n.AgentCapabilities != nil:
			item.Kind = OutlineCapabilities
			item.Status = string(n.AgentCapabilities.Status)
			item.Description = firstLine(n.AgentCapabilities.Description)
		case n.TrapType != nil:
			item.Kind = OutlineTrap
			item.Description = firstLine(n.TrapType.Description)
		}

		if n.Oid != nil && len(n.Oid.SubIdentifiers) > 0 {
			first := n.Oid.SubIdentifiers[0]
			if first.Name != nil {
				item.Parent = string(*first.Name)
			}
			last := n.Oid.SubIdentifiers[len(n.Oid.SubIdentifiers)-1]
			if last.Number != nil {
				item.SubID = fmt.Sprint(*last.Number)
			}
		}
		if n.SubIdentifier != nil {
			item.SubID = fmt.Sprint(*n.SubIdentifier)
		}

		out = append(out, item)
	}

	for i := range module.Body.Macros {
		m := &module.Body.Macros[i]
		out = append(out, OutlineItem{
			Name: string(m.Name), Kind: OutlineMacro,
			Line: m.Pos.Line, Column: m.Pos.Column,
		})
	}

	return out
}

// syntaxName renders a SYNTAX clause as the one word a reader scans for.
//
// Not the full clause: "INTEGER { up(1), down(2), testing(3) }" is the whole
// point of opening the definition, and putting it in the list turns a column
// into a paragraph.
func syntaxName(s *parser.Syntax) string {
	if s == nil {
		return ""
	}
	if s.Sequence != nil {
		return "SEQUENCE OF " + string(*s.Sequence)
	}
	// Type is a POINTER and the grammar is an alternation, so a syntax that
	// matched neither branch leaves both nil. Reading through it would be a
	// panic in the editor's own analysis path, on a file being typed — which is
	// to say on half-written input, which is all this ever sees.
	return syntaxTypeName(s.Type)
}

// syntaxTypeName is the same for a TEXTUAL-CONVENTION, whose SYNTAX is a bare
// SyntaxType rather than the alternation.
func syntaxTypeName(t *parser.SyntaxType) string {
	if t == nil {
		return ""
	}
	name := strings.TrimSpace(t.Name.String())
	if name == "" {
		return ""
	}
	if len(t.Enum) > 0 {
		return name + " {…}"
	}
	return name
}
