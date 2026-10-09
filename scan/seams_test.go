package scan

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/katayunak/1819/gofacts/fact"
)

func namedType(pkgPath, name string, under types.Type) *types.Named {
	pkg := types.NewPackage(pkgPath, pkgPath[strings.LastIndex(pkgPath, "/")+1:])
	return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), under, nil)
}

func signatureOf(params ...types.Type) *types.Signature {
	var vars []*types.Var
	for _, p := range params {
		vars = append(vars, types.NewVar(token.NoPos, nil, "", p))
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(vars...), nil, false)
}

func TestAGenericCallbackIsNotAPathToIO(t *testing.T) {
	if !genericSignature(signatureOf(types.Typ[types.Int32])) {
		t.Error("func(rune) rune links under CHA to every function of that shape")
	}
	if !genericSignature(signatureOf(types.Typ[types.Int], types.Typ[types.Int])) {
		t.Error("a sort comparator is plumbing")
	}
}

func TestACallbackNamingALibraryTypeIsFollowed(t *testing.T) {
	conn := namedType("github.com/go-pg/pg/v10/internal/pool", "Conn", types.NewStruct(nil, nil))
	ctx := namedType("context", "Context", types.NewInterfaceType(nil, nil))
	if genericSignature(signatureOf(ctx, types.NewPointer(conn))) {
		t.Error("go-pg runs every query inside func(context.Context, *pool.Conn); skipping it dropped real database seams")
	}
}

func TestStreamsAndErrorsAreNotIOAbstractions(t *testing.T) {
	for _, name := range []string{"error", "io.Writer", "io.Reader", "context.Context", "fmt.Stringer"} {
		if !plumbingIfaces[name] {
			t.Errorf("%s resolves under CHA to every implementation in the program", name)
		}
	}
	if plumbingIfaces["net.Conn"] {
		t.Error("net.Conn is how clients reach the socket; it has to be followed")
	}
}

func TestAnAsynchronousSendIsKeptWithoutAStaticPath(t *testing.T) {
	for _, name := range []string{"Publish", "SendByContext", "RequestWithContext", "Enqueue"} {
		if !asyncEffect(name) {
			t.Errorf("%s hands its write to a background goroutine", name)
		}
	}
	for _, name := range []string{"New", "WithDetails", "FromIncomingContext", "Get", "Pending", "Where", "SpanFromContext"} {
		if asyncEffect(name) {
			t.Errorf("%s is not a send", name)
		}
	}
}

func TestTxCloseWithoutCommitIsARollback(t *testing.T) {
	for _, target := range []string{
		"(*github.com/go-pg/pg/v10.Tx).Close",
		"(*database/sql.Tx).Close",
		"(*github.com/jmoiron/sqlx.Tx).Close",
	} {
		var f fact.Facts
		applyTxFacts(&f, target)
		if !f.RollsBackTx {
			t.Errorf("%s rolls back an uncommitted transaction", target)
		}
	}
	for _, target := range []string{"(*database/sql.DB).Close", "(*github.com/redis/go-redis/v9.Client).Close", "(*os.File).Close"} {
		var f fact.Facts
		applyTxFacts(&f, target)
		if f.RollsBackTx {
			t.Errorf("%s is not a rollback", target)
		}
	}
}

func TestRunInTransactionOpensCommitsAndRollsBack(t *testing.T) {
	for _, target := range []string{
		"(*github.com/go-pg/pg/v10.baseDB).RunInTransaction",
		"(*gorm.io/gorm.DB).Transaction",
		"(*github.com/uptrace/bun.DB).RunInTx",
		"github.com/jackc/pgx/v5.BeginFunc",
	} {
		var f fact.Facts
		applyTxFacts(&f, target)
		if !f.OpensTx || !f.CommitsTx || !f.RollsBackTx {
			t.Errorf("%s owns begin, commit and rollback: %+v", target, f)
		}
	}
}

func TestTxCloseSurvivesCursorNoiseFiltering(t *testing.T) {
	if isCursorNoise("Close", "(*github.com/go-pg/pg/v10.Tx).Close") {
		t.Fatal("Close on a transaction must reach applyTxFacts")
	}
	if !isCursorNoise("Close", "(*database/sql.DB).Close") || !isCursorNoise("Next", "(*database/sql.Rows).Next") {
		t.Fatal("pool close and cursor iteration stay suppressed")
	}
}
