package scan

import (
	"go/types"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/naming"
)

// extractReconcilers lists the module's functions and methods named as a
// repair job: by their own name (Reconcile, RunRecon), or as a run method of a
// type named like one ((*SettlementReconciler).Run). Test and generated code
// is left out, and so is a helper whose every non-test caller is itself a
// match ((*Job).Reconcile calling reconcileOne). Proof is the name, then what
// the body is seen to do; behaviour never decides whether a match is kept.
func (p *Program) extractReconcilers() []fact.Reconciler {
	named := map[string][]string{}
	for id, f := range p.funcs {
		if f.InTest || f.Generated {
			continue
		}
		if proof := reconcilerNamed(p.canon[id].Object().(*types.Func)); len(proof) > 0 {
			named[id] = proof
		}
	}
	var out []fact.Reconciler
	for id, proof := range named {
		if p.calledOnlyBy(id, named) {
			continue
		}
		f := p.funcs[id]
		out = append(out, fact.Reconciler{Func: f.Ref, Proof: append(proof, behaviour(f.Facts)...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Func.ID() < out[j].Func.ID() })
	return out
}

// reconcilerNamed says which names make fn a reconciler, if any. The receiver
// is read from its type, so a pointer, alias or generic receiver is named by
// the type it is.
func reconcilerNamed(fn *types.Func) []string {
	var proof []string
	if words := naming.ReconcilerName(fn.Name()); len(words) > 0 {
		quoted := make([]string, len(words))
		for i, w := range words {
			quoted[i] = strconv.Quote(w)
		}
		proof = append(proof, "name has "+strings.Join(quoted, " and "))
	}
	if recv := fn.Signature().Recv(); recv != nil && naming.IsRunMethod(fn.Name()) {
		if t := namedOf(recv.Type()); t != nil && naming.IsReconcilerType(t.Obj().Name()) {
			proof = append(proof, "receiver "+t.Obj().Name())
		}
	}
	return proof
}

// calledOnlyBy reports whether id has a non-test caller and every one is in
// set: a helper of those, not a job of its own.
func (p *Program) calledOnlyBy(id string, set map[string][]string) bool {
	called := false
	for _, e := range p.inEdges[id] {
		if f := p.funcs[e.From]; f == nil || f.InTest {
			continue
		}
		if _, ok := set[e.From]; !ok {
			return false
		}
		called = true
	}
	return called
}

// behaviour is evidence beside the name. TouchesDB and TouchesNet are the
// function's own calls; Reaches also counts its callees, which is where a
// reconciler that goes through a repository does its work.
func behaviour(f fact.Facts) []string {
	var out []string
	switch {
	case f.TouchesDB:
		out = append(out, "touches the database")
	case slices.Contains(f.Reaches, fact.SeamDB):
		out = append(out, "reaches the database through a callee")
	}
	switch {
	case f.TouchesNet:
		out = append(out, "calls the network")
	case slices.ContainsFunc(f.Reaches, fact.SeamKind.Network):
		out = append(out, "reaches the network through a callee")
	}
	if len(f.WritesStatus) > 0 {
		out = append(out, "writes status "+strings.Join(f.WritesStatus, ", "))
	}
	return out
}
