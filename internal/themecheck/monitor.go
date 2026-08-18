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
// excluded so explicit palettes remain useful as high-contrast fixtures. Raw
// color analysis resolves string constants and concatenations, follows static
// string-returning helper functions, and identifies function parameters that
// flow transitively into lipgloss.Color. It deliberately stops at dynamic
// runtime values, which are the legitimate Theme-slot path.
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
	rawColors := rawColorAnalyses(files)
	var findings []Finding
	for _, file := range files {
		packageDependent := dependent[file.packageKey]
		if _, allowed := RawColorAllowlist[file.path]; !allowed {
			analysis := rawColors[file.packageKey]
			for _, decl := range file.file.Decls {
				constants := analysis.constants
				node := ast.Node(decl)
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
					constants = mergedConstants(analysis.constants, constantsIn(fn.Body))
					node = fn.Body
				}
				findings = append(findings, rawColorFindings(fset, file, node, constants, analysis)...)
			}
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

func rawColorFindings(fset *token.FileSet, file parsedFile, node ast.Node, constants map[string]ast.Expr, analysis rawColorAnalysis) []Finding {
	var findings []Finding
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		var sinkArgs []int
		if isLipglossCall(call, file.lipglossAlias, "Color") {
			sinkArgs = []int{0}
		} else if id, ok := call.Fun.(*ast.Ident); ok {
			for index := range analysis.wrapperParams[id.Name] {
				sinkArgs = append(sinkArgs, index)
			}
		}
		for _, index := range sinkArgs {
			if index >= len(call.Args) {
				continue
			}
			value, ok := resolveStaticString(call.Args[index], constants, analysis.staticStrings, nil)
			if !ok {
				continue
			}
			findings = append(findings, finding(fset, file.path, call.Args[index], "raw color",
				fmt.Sprintf("static color %q reaches lipgloss.Color; use a Theme slot", value)))
		}
		return true
	})
	return findings
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

type rawColorAnalysis struct {
	constants     map[string]ast.Expr
	staticStrings map[string]string
	wrapperParams map[string]map[int]bool
}

type functionBody struct {
	decl  *ast.FuncDecl
	alias string
}

func rawColorAnalyses(files []parsedFile) map[string]rawColorAnalysis {
	constants := map[string]map[string]ast.Expr{}
	functions := map[string]map[string]functionBody{}
	for _, file := range files {
		if constants[file.packageKey] == nil {
			constants[file.packageKey] = map[string]ast.Expr{}
			functions[file.packageKey] = map[string]functionBody{}
		}
		for _, decl := range file.file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
				for name, value := range constantsIn(gen) {
					constants[file.packageKey][name] = value
				}
			}
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || fn.Name.Name == "init" {
				continue
			}
			functions[file.packageKey][fn.Name.Name] = functionBody{decl: fn, alias: file.lipglossAlias}
		}
	}

	result := map[string]rawColorAnalysis{}
	for packageKey, packageFunctions := range functions {
		analysis := rawColorAnalysis{
			constants: constants[packageKey], staticStrings: map[string]string{}, wrapperParams: map[string]map[int]bool{},
		}
		for {
			grew := false
			for name, fn := range packageFunctions {
				if _, known := analysis.staticStrings[name]; known {
					continue
				}
				functionConstants := mergedConstants(analysis.constants, constantsIn(fn.decl.Body))
				if value, ok := staticReturn(fn.decl.Body, functionConstants, analysis.staticStrings); ok {
					analysis.staticStrings[name] = value
					grew = true
				}
			}
			if !grew {
				break
			}
		}
		for {
			grew := false
			for name, fn := range packageFunctions {
				params := functionParameters(fn.decl)
				for index, parameter := range params {
					if analysis.wrapperParams[name][index] || !parameterFlowsToColor(fn.decl.Body, parameter, fn.alias, analysis.wrapperParams) {
						continue
					}
					if analysis.wrapperParams[name] == nil {
						analysis.wrapperParams[name] = map[int]bool{}
					}
					analysis.wrapperParams[name][index] = true
					grew = true
				}
			}
			if !grew {
				break
			}
		}
		result[packageKey] = analysis
	}
	return result
}

func constantsIn(node ast.Node) map[string]ast.Expr {
	result := map[string]ast.Expr{}
	ast.Inspect(node, func(node ast.Node) bool {
		decl, ok := node.(*ast.GenDecl)
		if !ok || decl.Tok != token.CONST {
			return true
		}
		for _, spec := range decl.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if i < len(value.Values) {
					result[name.Name] = value.Values[i]
				}
			}
		}
		return false
	})
	return result
}

func mergedConstants(base, extra map[string]ast.Expr) map[string]ast.Expr {
	if len(extra) == 0 {
		return base
	}
	result := make(map[string]ast.Expr, len(base)+len(extra))
	for name, value := range base {
		result[name] = value
	}
	for name, value := range extra {
		result[name] = value
	}
	return result
}

func staticReturn(body *ast.BlockStmt, constants map[string]ast.Expr, functions map[string]string) (string, bool) {
	var value string
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		ret, ok := node.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		value, found = resolveStaticString(ret.Results[0], constants, functions, nil)
		return !found
	})
	return value, found
}

func resolveStaticString(expr ast.Expr, constants map[string]ast.Expr, functions map[string]string, seen map[string]bool) (string, bool) {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(expr.Value)
		return value, err == nil
	case *ast.ParenExpr:
		return resolveStaticString(expr.X, constants, functions, seen)
	case *ast.BinaryExpr:
		if expr.Op != token.ADD {
			return "", false
		}
		left, leftOK := resolveStaticString(expr.X, constants, functions, seen)
		right, rightOK := resolveStaticString(expr.Y, constants, functions, seen)
		return left + right, leftOK && rightOK
	case *ast.Ident:
		if seen == nil {
			seen = map[string]bool{}
		}
		if seen[expr.Name] {
			return "", false
		}
		value, ok := constants[expr.Name]
		if !ok {
			return "", false
		}
		seen[expr.Name] = true
		resolved, ok := resolveStaticString(value, constants, functions, seen)
		delete(seen, expr.Name)
		return resolved, ok
	case *ast.CallExpr:
		if len(expr.Args) != 0 {
			return "", false
		}
		id, ok := expr.Fun.(*ast.Ident)
		if !ok {
			return "", false
		}
		value, ok := functions[id.Name]
		return value, ok
	default:
		return "", false
	}
}

func functionParameters(fn *ast.FuncDecl) []string {
	var result []string
	if fn.Type.Params == nil {
		return result
	}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			result = append(result, name.Name)
		}
	}
	return result
}

func parameterFlowsToColor(body *ast.BlockStmt, parameter, lipglossAlias string, wrappers map[string]map[int]bool) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		var sinkArgs []int
		if isLipglossCall(call, lipglossAlias, "Color") {
			sinkArgs = []int{0}
		} else if id, ok := call.Fun.(*ast.Ident); ok {
			for index := range wrappers[id.Name] {
				sinkArgs = append(sinkArgs, index)
			}
		}
		for _, index := range sinkArgs {
			if index < len(call.Args) && expressionContainsIdentifier(call.Args[index], parameter) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func expressionContainsIdentifier(expr ast.Expr, name string) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && id.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}

func themeDependentFunctions(files []parsedFile) map[string]map[string]bool {
	type themeFunctionBody struct {
		node  ast.Node
		alias string
	}
	bodies := map[string]map[string]themeFunctionBody{}
	for _, file := range files {
		if bodies[file.packageKey] == nil {
			bodies[file.packageKey] = map[string]themeFunctionBody{}
		}
		for _, decl := range file.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || fn.Name.Name == "init" {
				continue
			}
			bodies[file.packageKey][fn.Name.Name] = themeFunctionBody{fn.Body, file.lipglossAlias}
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
