// Package silenterr is a go/analysis analyzer that flags silent error swallows
// in methods on *App — the Wails-bound surface whose errors are user-visible.
// Annotate with "// silenterr:ok <reason>" on the line immediately above the
// assignment to allow a specific swallow with a written justification.
package silenterr

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const directive = "silenterr:ok"

// Analyzer flags silent error swallows (_ = f()) in *App methods.
// Scope is any type named App; in this repo that is uniquely the Wails boundary.
var Analyzer = &analysis.Analyzer{
	Name:     "silenterr",
	Doc:      "flags silent error swallows in *App methods; annotate with // silenterr:ok <reason> to allow",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

type fileLine struct {
	file string
	line int
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	comments := buildCommentIndex(pass.Files, pass.Fset)

	nodeFilter := []ast.Node{(*ast.FuncDecl)(nil)}
	insp.Preorder(nodeFilter, func(n ast.Node) {
		fn := n.(*ast.FuncDecl)
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			return
		}
		if !isStarApp(fn.Recv.List[0].Type, pass.TypesInfo) {
			return
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			checkAssign(pass, assign, comments)
			return true
		})
	})
	return nil, nil
}

func checkAssign(pass *analysis.Pass, assign *ast.AssignStmt, comments map[fileLine]string) {
	if !hasErrorBlank(pass, assign) {
		return
	}

	pos := pass.Fset.Position(assign.Pos())
	preceding := fileLine{file: pos.Filename, line: pos.Line - 1}
	if commentText, ok := comments[preceding]; ok {
		isDir, justification := parseDirective(commentText)
		if isDir {
			if justification == "" {
				pass.Reportf(assign.Pos(), "silent error swallow allow-listed without justification")
			}
			return
		}
	}
	pass.Reportf(assign.Pos(), "silent error swallow in *App method; add // silenterr:ok <reason> to allow it")
}

// hasErrorBlank returns true when any blank identifier on the LHS aligns with
// an error-typed result position on the RHS.
func hasErrorBlank(pass *analysis.Pass, assign *ast.AssignStmt) bool {
	results := rhsResultTypes(pass, assign)
	if results == nil {
		return false
	}
	for i, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok || ident.Name != "_" {
			continue
		}
		if i < len(results) && results[i] != nil && isErrorType(results[i]) {
			return true
		}
	}
	return false
}

// rhsResultTypes returns the type at each result position for the assignment RHS.
// Returns nil when the shape is not analyzable.
func rhsResultTypes(pass *analysis.Pass, assign *ast.AssignStmt) []types.Type {
	if len(assign.Rhs) == 1 {
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return nil
		}
		sig := callSignature(pass, call)
		if sig == nil {
			return nil
		}
		out := make([]types.Type, sig.Results().Len())
		for i := range out {
			out[i] = sig.Results().At(i).Type()
		}
		return out
	}
	if len(assign.Rhs) == len(assign.Lhs) {
		out := make([]types.Type, len(assign.Rhs))
		for i, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}
			sig := callSignature(pass, call)
			if sig == nil || sig.Results().Len() != 1 {
				continue
			}
			out[i] = sig.Results().At(0).Type()
		}
		return out
	}
	return nil
}

func callSignature(pass *analysis.Pass, call *ast.CallExpr) *types.Signature {
	tv, ok := pass.TypesInfo.Types[call.Fun]
	if !ok {
		return nil
	}
	sig, _ := tv.Type.(*types.Signature)
	return sig
}

// isStarApp reports whether expr is *App (pointer to a named type named "App").
func isStarApp(expr ast.Expr, info *types.Info) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return false
	}
	obj := info.Uses[ident]
	if obj == nil {
		return false
	}
	return obj.Name() == "App"
}

func isErrorType(t types.Type) bool {
	return t == types.Universe.Lookup("error").Type()
}

func buildCommentIndex(files []*ast.File, fset *token.FileSet) map[fileLine]string {
	m := make(map[fileLine]string)
	for _, f := range files {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				p := fset.Position(c.Pos())
				m[fileLine{p.Filename, p.Line}] = c.Text
			}
		}
	}
	return m
}

func parseDirective(commentText string) (isDirective bool, justification string) {
	text := strings.TrimPrefix(commentText, "//")
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, directive) {
		return false, ""
	}
	rest := strings.TrimPrefix(text, directive)
	rest = strings.TrimSpace(rest)
	// Strip optional leading punctuation separators (—, -, :).
	rest = strings.TrimLeft(rest, "—-:")
	rest = strings.TrimSpace(rest)
	return true, rest
}
