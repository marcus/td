// Package themecheck guards the embedded monitor against presentation state
// that cannot follow a live, model-scoped theme.
package themecheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

const lipglossPath = "charm.land/lipgloss/v2"

// RawColorAllowlist is deliberately limited to the functions that define or
// derive td's standalone defaults. Renderers and stateful models do not belong
// here: their colors must come from their model's Theme.
var RawColorAllowlist = map[string]string{
	"pkg/monitor/theme.go":        "monitor default palette and model-owned style derivation",
	"pkg/monitor/modal/styles.go": "declarative modal default palette and instance style derivation",
	"pkg/monitor/markdown.go":     "legacy standalone markdown defaults and palette derivation",
}

// FrozenThemeAllowlist contains source-compatibility globals that are not used
// by a live Model. New entries require the same explicit API rationale.
var FrozenThemeAllowlist = map[string]string{
	"pkg/monitor/overlay.go#DimStyle": "deprecated standalone OverlayModal API; live Model rendering bypasses it",
}

// Finding identifies a frozen package-init style or an unexplained raw color.
type Finding struct {
	File string
	Line int
	Kind string
	Text string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", f.File, f.Line, f.Kind, f.Text)
}

// ScanMonitor audits every production Go file below pkg/monitor. Tests are
// excluded so explicit palettes remain useful as high-contrast fixtures.
func ScanMonitor(root string) ([]Finding, error) {
	monitorDir := filepath.Join(root, "pkg", "monitor")
	var files []parsedFile
	fset := token.NewFileSet()
	err := filepath.WalkDir(monitorDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, parsedFile{
			path: filepath.ToSlash(rel), packageKey: filepath.ToSlash(filepath.Dir(rel)),
			file: file, lipglossAlias: importAlias(file, lipglossPath),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	dependent := themeDependentFunctions(files)
	var findings []Finding
	for _, file := range files {
		packageDependent := dependent[file.packageKey]
		if _, allowed := RawColorAllowlist[file.path]; !allowed {
			ast.Inspect(file.file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !isLipglossCall(call, file.lipglossAlias, "Color") || len(call.Args) != 1 {
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				findings = append(findings, finding(fset, file.path, literal, "raw color", "lipgloss.Color must read a Theme slot"))
				return true
			})
		}

		for _, decl := range file.file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl.Tok != token.VAR {
					continue
				}
				for _, spec := range decl.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, expr := range value.Values {
						if usesThemeDerivation(expr, file.lipglossAlias, packageDependent) {
							if frozenValueAllowed(file.path, value) {
								continue
							}
							findings = append(findings, finding(fset, file.path, expr, "frozen theme", "package-level value captures theme-derived presentation state"))
						}
					}
				}
			case *ast.FuncDecl:
				if decl.Name.Name == "init" && decl.Body != nil && usesThemeDerivation(decl.Body, file.lipglossAlias, packageDependent) {
					findings = append(findings, finding(fset, file.path, decl, "frozen theme", "init captures theme-derived presentation state"))
				}
			}
		}
	}
	return findings, nil
}

func frozenValueAllowed(path string, value *ast.ValueSpec) bool {
	if len(value.Names) != 1 || len(value.Values) != 1 {
		return false
	}
	_, ok := FrozenThemeAllowlist[path+"#"+value.Names[0].Name]
	return ok
}

type parsedFile struct {
	path          string
	packageKey    string
	file          *ast.File
	lipglossAlias string
}

func themeDependentFunctions(files []parsedFile) map[string]map[string]bool {
	type functionBody struct {
		node  ast.Node
		alias string
	}
	bodies := map[string]map[string]functionBody{}
	for _, file := range files {
		if bodies[file.packageKey] == nil {
			bodies[file.packageKey] = map[string]functionBody{}
		}
		for _, decl := range file.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || fn.Name.Name == "init" {
				continue
			}
			bodies[file.packageKey][fn.Name.Name] = functionBody{fn.Body, file.lipglossAlias}
		}
	}

	result := map[string]map[string]bool{}
	for packageKey, packageBodies := range bodies {
		dependent := map[string]bool{}
		for {
			grew := false
			for name, body := range packageBodies {
				if dependent[name] || !usesThemeDerivation(body.node, body.alias, dependent) {
					continue
				}
				dependent[name] = true
				grew = true
			}
			if !grew {
				break
			}
		}
		result[packageKey] = dependent
	}
	return result
}

func usesThemeDerivation(node ast.Node, lipglossAlias string, dependent map[string]bool) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isLipglossCall(call, lipglossAlias, "Color") || isLipglossCall(call, lipglossAlias, "NewStyle") {
			found = true
			return false
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			if dependent[id.Name] || strings.Contains(id.Name, "Theme") || strings.Contains(id.Name, "theme") {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isLipglossCall(call *ast.CallExpr, alias, name string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	id, ok := selector.X.(*ast.Ident)
	return ok && alias != "" && id.Name == alias
}

func importAlias(file *ast.File, importPath string) string {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != importPath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		if importPath == lipglossPath {
			return "lipgloss"
		}
		return filepath.Base(importPath)
	}
	return ""
}

func finding(fset *token.FileSet, path string, node ast.Node, kind, text string) Finding {
	return Finding{File: path, Line: fset.Position(node.Pos()).Line, Kind: kind, Text: text}
}
