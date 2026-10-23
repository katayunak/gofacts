package scan

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// roundTripper is the interface a test fakes a concrete *http.Client through:
// the client's Transport.
const roundTripper = "net/http.RoundTripper"

// clientHandedIn names the parameter or field through which a test can give a
// call on a *http.Client its own client, one whose Transport fails or stalls.
// It is empty for http.DefaultClient, package variables, a client returned by
// a call and a client built in the function: replacing those means changing
// state every test shares, or is not possible at all. A client built in the
// function still counts when its Transport is handed in.
func clientHandedIn(c *ssa.CallCommon, callee *ssa.Function) string {
	if receiverName(callee) != "Client" || !netHTTPClient[callee.String()] || len(c.Args) == 0 {
		return ""
	}
	return handedIn(c.Args[0], map[ssa.Value]bool{})
}

// handedIn names the parameter or field through which a test supplies v, or
// what v points to, from outside the function. A parameter of a closure does
// not count: the module calls the closure, not the test.
func handedIn(v ssa.Value, seen map[ssa.Value]bool) string {
	if v == nil || seen[v] {
		return ""
	}
	seen[v] = true
	switch v := v.(type) {
	case *ssa.Parameter:
		if v.Parent().Parent() == nil {
			return "parameter " + v.Name()
		}
	case *ssa.FreeVar:
		return handedIn(boundTo(v), seen)
	case *ssa.FieldAddr:
		return fieldOf(v.X, v.Field, seen)
	case *ssa.Field:
		return fieldOf(v.X, v.Field, seen)
	case *ssa.UnOp:
		if v.Op == token.MUL {
			return loadedFrom(v.X, seen)
		}
	case *ssa.Phi:
		for _, e := range v.Edges {
			if s := handedIn(e, seen); s != "" {
				return s
			}
		}
	case *ssa.ChangeType:
		return handedIn(v.X, seen)
	case *ssa.MakeInterface:
		return handedIn(v.X, seen)
	case *ssa.TypeAssert:
		return handedIn(v.X, seen)
	case *ssa.Extract:
		if ta, ok := v.Tuple.(*ssa.TypeAssert); ok {
			return handedIn(ta.X, seen)
		}
	case *ssa.Alloc:
		if i := transportField(v.Type()); i >= 0 {
			if s := storedIn(v, seen); s != "" {
				return s
			}
			return storedInField(v, i, seen)
		}
	}
	return ""
}

// loadedFrom is handedIn for the value read from addr.
func loadedFrom(addr ssa.Value, seen map[ssa.Value]bool) string {
	switch a := addr.(type) {
	case *ssa.Global:
		return ""
	case *ssa.Alloc:
		return storedIn(a, seen)
	case *ssa.FieldAddr:
		if s := handedIn(a, seen); s != "" {
			return s
		}
		if alloc, ok := a.X.(*ssa.Alloc); ok {
			return storedInField(alloc, a.Field, seen)
		}
		return ""
	case *ssa.FreeVar:
		return loadedFrom(boundTo(a), seen)
	}
	return handedIn(addr, seen)
}

func fieldOf(x ssa.Value, i int, seen map[ssa.Value]bool) string {
	if handedIn(x, seen) == "" {
		return ""
	}
	return "field " + fieldName(x.Type(), i)
}

// storedIn is handedIn for any value stored at addr.
func storedIn(addr ssa.Value, seen map[ssa.Value]bool) string {
	refs := addr.Referrers()
	if refs == nil {
		return ""
	}
	for _, r := range *refs {
		if st, ok := r.(*ssa.Store); ok && st.Addr == addr {
			if s := handedIn(st.Val, seen); s != "" {
				return s
			}
		}
	}
	return ""
}

// storedInField is handedIn for any value stored in field i of what alloc
// points to.
func storedInField(alloc *ssa.Alloc, i int, seen map[ssa.Value]bool) string {
	refs := alloc.Referrers()
	if refs == nil {
		return ""
	}
	for _, r := range *refs {
		if fa, ok := r.(*ssa.FieldAddr); ok && fa.X == alloc && fa.Field == i {
			if s := storedIn(fa, seen); s != "" {
				return s
			}
		}
	}
	return ""
}

// boundTo is the value the enclosing function captured for fv.
func boundTo(fv *ssa.FreeVar) ssa.Value {
	fn := fv.Parent()
	i := slices.Index(fn.FreeVars, fv)
	refs := fn.Referrers()
	if i < 0 || refs == nil {
		return nil
	}
	for _, r := range *refs {
		if mc, ok := r.(*ssa.MakeClosure); ok && mc.Fn == fn && i < len(mc.Bindings) {
			return mc.Bindings[i]
		}
	}
	return nil
}

// transportField is the index of http.Client's Transport field when t points
// to an http.Client, and -1 otherwise.
func transportField(t types.Type) int {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return -1
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "net/http" || n.Obj().Name() != "Client" {
		return -1
	}
	st, ok := n.Underlying().(*types.Struct)
	if !ok {
		return -1
	}
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == "Transport" {
			return i
		}
	}
	return -1
}

// fieldName is field i of the struct t is or points to, qualified by the
// struct's type when it has a name.
func fieldName(t types.Type, i int) string {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok || i < 0 || i >= st.NumFields() {
		return "?"
	}
	name := st.Field(i).Name()
	if n, ok := t.(*types.Named); ok {
		return types.TypeString(n, relativeTo) + "." + name
	}
	return name
}
