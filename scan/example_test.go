package scan_test

import (
	"context"
	"fmt"
	"log"

	"github.com/katayunak/1819/gofacts/fact"
	"github.com/katayunak/1819/gofacts/scan"
)

func Example() {
	prog, err := scan.Load(context.Background(), scan.Options{Dir: "testdata/paysvc"})
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range prog.Findings() {
		if f.Severity == fact.SevCritical {
			fmt.Printf("%s %s:%d %s\n", f.ID, f.Ref.File, f.Line, f.Basis)
		}
	}

	fn, _ := prog.FunctionAt("api/server.go", 64)
	fmt.Println("line 64 is in", fn.Ref.Symbol)
	for _, e := range prog.Callers(fn.Ref.ID()) {
		fmt.Println("called by", e.From, e.Certainty)
	}
	// Output:
	// IDEM-KEY-NOT-UNIQUE domain/payment.go:23 heuristic
	// MONEY-FLOAT api/server.go:93 heuristic
	// MONEY-FLOAT domain/payment.go:24 heuristic
	// TX-NET-CALL api/server.go:64 heuristic
	// line 64 is in (*Server).process
	// called by example.com/paysvc/api#(*Server).CreatePayment certain
}
