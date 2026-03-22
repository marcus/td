package diff

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// ChangeKind describes what happened to a symbol.
type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeRemoved  ChangeKind = "removed"
	ChangeModified ChangeKind = "modified"
)

// ChangeCategory describes the type of symbol that changed.
type ChangeCategory string

const (
	CategoryFunction ChangeCategory = "function"
	CategoryMethod   ChangeCategory = "method"
	CategoryType     ChangeCategory = "type"
	CategoryImport   ChangeCategory = "import"
	CategoryConst    ChangeCategory = "const"
	CategoryVar      ChangeCategory = "var"
)

// Change represents a semantic change to a symbol.
type Change struct {
	Kind     ChangeKind     `json:"kind"`
	Symbol   string         `json:"symbol"`
	Category ChangeCategory `json:"category"`
	Detail   string         `json:"detail,omitempty"`
}

// AnalyzeGoFile compares old and new Go source to identify symbol-level changes.
// Either oldSrc or newSrc may be nil (for added/deleted files).
func AnalyzeGoFile(oldSrc, newSrc []byte) []Change {
	oldSymbols := extractSymbols(oldSrc)
	newSymbols := extractSymbols(newSrc)

	var changes []Change

	// Find removed and modified symbols
	for key, oldSym := range oldSymbols {
		if newSym, ok := newSymbols[key]; ok {
			if oldSym.signature != newSym.signature {
				changes = append(changes, Change{
					Kind:     ChangeModified,
					Symbol:   oldSym.name,
					Category: oldSym.category,
					Detail:   fmt.Sprintf("signature changed: %s → %s", oldSym.signature, newSym.signature),
				})
			}
		} else {
			changes = append(changes, Change{
				Kind:     ChangeRemoved,
				Symbol:   oldSym.name,
				Category: oldSym.category,
			})
		}
	}

	// Find added symbols
	for key, newSym := range newSymbols {
		if _, ok := oldSymbols[key]; !ok {
			changes = append(changes, Change{
				Kind:     ChangeAdded,
				Symbol:   newSym.name,
				Category: newSym.category,
			})
		}
	}

	return changes
}

type symbolInfo struct {
	name      string
	category  ChangeCategory
	signature string
}

func extractSymbols(src []byte) map[string]symbolInfo {
	symbols := make(map[string]symbolInfo)
	if len(src) == 0 {
		return symbols
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.AllErrors)
	if err != nil || f == nil {
		return symbols
	}

	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := path
		if imp.Name != nil {
			name = imp.Name.Name
		}
		key := "import:" + path
		symbols[key] = symbolInfo{name: path, category: CategoryImport, signature: name}
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				recv := receiverType(d.Recv)
				name := recv + "." + d.Name.Name
				key := "method:" + name
				symbols[key] = symbolInfo{
					name:      name,
					category:  CategoryMethod,
					signature: funcSignature(d),
				}
			} else {
				key := "func:" + d.Name.Name
				symbols[key] = symbolInfo{
					name:      d.Name.Name,
					category:  CategoryFunction,
					signature: funcSignature(d),
				}
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					key := "type:" + s.Name.Name
					symbols[key] = symbolInfo{
						name:      s.Name.Name,
						category:  CategoryType,
						signature: typeSignature(s),
					}
				case *ast.ValueSpec:
					cat := CategoryVar
					if d.Tok == token.CONST {
						cat = CategoryConst
					}
					for _, n := range s.Names {
						key := string(cat) + ":" + n.Name
						symbols[key] = symbolInfo{
							name:      n.Name,
							category:  cat,
							signature: valueSignature(s),
						}
					}
				}
			}
		}
	}

	return symbols
}

func receiverType(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	t := fields.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if ident, ok := t.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func funcSignature(d *ast.FuncDecl) string {
	var parts []string
	if d.Type.Params != nil {
		for _, p := range d.Type.Params.List {
			parts = append(parts, exprString(p.Type))
		}
	}
	sig := "(" + strings.Join(parts, ", ") + ")"
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		var results []string
		for _, r := range d.Type.Results.List {
			results = append(results, exprString(r.Type))
		}
		if len(results) == 1 {
			sig += " " + results[0]
		} else {
			sig += " (" + strings.Join(results, ", ") + ")"
		}
	}
	return sig
}

func typeSignature(s *ast.TypeSpec) string {
	return exprString(s.Type)
}

func valueSignature(s *ast.ValueSpec) string {
	if s.Type != nil {
		return exprString(s.Type)
	}
	return ""
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		if t.Fields == nil || len(t.Fields.List) == 0 {
			return "struct{}"
		}
		var fields []string
		for _, f := range t.Fields.List {
			ft := exprString(f.Type)
			for _, n := range f.Names {
				fields = append(fields, n.Name+" "+ft)
			}
			if len(f.Names) == 0 {
				fields = append(fields, ft)
			}
		}
		return "struct{" + strings.Join(fields, "; ") + "}"
	case *ast.ChanType:
		return "chan " + exprString(t.Value)
	case *ast.FuncType:
		return "func()"
	case *ast.Ellipsis:
		return "..." + exprString(t.Elt)
	default:
		return "?"
	}
}
