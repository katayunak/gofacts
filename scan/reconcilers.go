package scan

import (
	"fmt"
	"go/ast"
	"sort"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/naming"
)

// extractReconcilers lists the module's functions and methods named as a
// repair job. Only the name says so, so every one is Heuristic; generated
// files are left out.
func (p *Program) extractReconcilers() []fact.Reconciler {
	var out []fact.Reconciler
	for _, pkg := range p.prod {
		for _, f := range pkg.Syntax {
			if p.generated[p.rel(f.Pos())] {
				continue
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || !naming.IsReconcilerName(fn.Name.Name) {
					continue
				}
				out = append(out, fact.Reconciler{
					Func: fact.CodeRef{
						Pkg: pkg.PkgPath, Symbol: declSymbol(pkg, fn), File: p.rel(fn.Pos()), Line: p.line(fn.Pos()),
					},
					Basis: fact.Heuristic,
					Proof: fmt.Sprintf("named %s", fn.Name.Name),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Func.ID() < out[j].Func.ID() })
	return out
}
