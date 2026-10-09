# gofacts

[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)

Facts a Go payment service's code can prove about itself — as a library.

gofacts loads a Go module, type-checks it, builds SSA and a call graph, and
answers the questions that matter for code that moves money:

- which functions call which, and **how sure** that is
- where calls **leave the process** (database, HTTP, gRPC, queues, cache, clock, randomness) and whether a test can fake them
- which **database transactions** are open around which calls, which returns leak them, and which writes bypass them
- which **lifecycles** (status enums) exist, where each state is produced, and from which state
- what the **schema** enforces after every migration has run — tables, unique constraints (composite and partial), foreign keys, checks
- how each **table is written**: insert-only, updated in Go, updated in the database, upserted
- which field is the **idempotency key**, and whether the database refuses a duplicate of it
- where **money** is stored in a float, divided without a rounding rule, or kept without a currency

Every answer carries a file and line, and says whether it was **proved** by the
compiler or **guessed** from names. It never runs the code it reads.

It is the shared core of two tools: [testigo](https://github.com/katayunak/testigo)
plans payment-flow tests from these facts, and the [1819](../README.md) reviewer
judges diffs against them.

## Install

```bash
go get github.com/katayunak/1819/gofacts
```

Go 1.25 or newer. The only dependency is `golang.org/x/tools`.

## Quick start

```go
prog, err := scan.Load(ctx, scan.Options{Dir: "path/to/service"})
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
```

On the bundled fixture service ([`scan/testdata/paysvc`](scan/testdata/paysvc)) this prints:

```
IDEM-KEY-NOT-UNIQUE domain/payment.go:23 heuristic
MONEY-FLOAT api/server.go:93 heuristic
MONEY-FLOAT domain/payment.go:24 heuristic
TX-NET-CALL api/server.go:64 heuristic
line 64 is in (*Server).process
called by example.com/paysvc/api#(*Server).CreatePayment certain
```

The snippet is [`scan/example_test.go`](scan/example_test.go) and runs with the tests, so it cannot drift.

## What you can ask

`scan.Load` builds a `*scan.Program` once; everything else is a read.

| method | returns |
|---|---|
| `Functions()`, `Function(id)` | every declared function of the module, with facts about its body |
| `FunctionAt(file, line)` | the function a line belongs to — how a diff hunk maps to code |
| `Callers(id)`, `Callees(id)`, `Edges()` | resolved call edges, each with `Certainty`, and whether it is a `go` or `defer` hand-off |
| `TestsFor(id, maxHops)` | the tests that reach a function, nearest first (load with `Options{Tests: true}`) |
| `Seams()` | calls that leave the process, and whether they are injectable |
| `Transactions()` | each transaction: begin, commits, rollbacks, leaking returns, what runs while it is open |
| `States()` | lifecycles with their states, every write site, source states from guards, whether the write is inside a transaction |
| `Infra()` | the schema after all migrations, with `UniqueOn(table, column)` for uniqueness checks |
| `TableWrites()` | how each table is written |
| `IdempotencyKeys()`, `MoneyTypes()` | scored candidates that show their evidence for and against |
| `Findings()` | every rule below, applied to the whole module |
| `Flow(entries)` | the part reachable from declared entry points, in the shape testigo stores as `flow.json` |
| `Issues()` | packages that did not load or type-check |

Function IDs look like `example.com/paysvc/api#(*Server).process`.

## Findings

| ID | severity | basis | what it means |
|---|---|---|---|
| `TX-NET-CALL` | critical | proved / heuristic | a network call while a database transaction is open — locks held for someone else's timeout, and a rollback cannot undo the remote effect |
| `TX-NO-ROLLBACK` | high | proved | a return between begin and commit reaches neither commit nor rollback, and nothing is deferred |
| `TX-UNUSED` | high | proved | a transaction is opened and committed but no statement runs on it, so nothing it was meant to protect is atomic |
| `TX-BYPASS` | high / medium | proved / heuristic | database work runs beside the open transaction instead of in it |
| `SEAM-CONCRETE` | high | proved | I/O called on a concrete type, so a test cannot make it fail or time out |
| `IDEM-KEY-NOT-UNIQUE` | critical | heuristic | the idempotency key's column has no unique constraint, so two concurrent requests both pass the "seen it?" check |
| `MONEY-FLOAT` | critical | heuristic | money stored, accepted, returned or converted as a float |
| `MONEY-DIV` | high | heuristic | integer money divided with no stated rounding rule |
| `MONEY-NO-CURRENCY` | medium | heuristic | a bare integer amount with no currency beside it |
| `STATE-NEVER-SET` | medium | proved / heuristic | a declared state nothing in the module produces |
| `SCHEMA-NOT-FOUND` | critical | proved | the module talks to a database but no schema was found, so uniqueness claims are withheld |
| `MIGRATION-NO-DOWN` | medium | proved | migrations, under a tool that expects them, with no down step |
| `LOAD-ERROR` | info | proved | a package did not load; facts behind it are missing, not absent |

A finding is evidence of a risk, not proof of a bug.

## Proved, guessed, and how sure

**`Basis`.** *Proved* follows from the type checker, SSA or the schema as
written: a static call, a return path, a constraint. *Heuristic* depends on
names or package tables: that `PSP.Authorize` is an HTTP call, that `Amount` is
money. A heuristic finding is wrong when code is named unusually; a proved one
is wrong only if gofacts has a bug.

**`Certainty`** of a call edge:

| | |
|---|---|
| `certain` | a static call — the compiler names one callee |
| `likely` | a dynamic call narrowed (VTA) to the implementations the module is seen to store there |
| `possible` | a dynamic call nothing in the module feeds, so every implementation (CHA) |

A call chain is as certain as its weakest edge. None of it is a runtime trace.

## Packages

| package | what it does |
|---|---|
| [`scan`](scan) | builds the `Program` and every analysis above |
| [`fact`](fact) | the vocabulary: functions, edges, seams, transactions, states, schema, findings, `Flow`. Standard library only, so consumers can share facts without importing the analyser |
| [`load`](load) | type-checks a module with go/packages; load problems become located `fact.LoadIssue`s |
| [`goenv`](goenv) | the environment the go command runs in when pointed at someone else's repository |
| [`gocmd`](gocmd) | runs `go build`, `vet` and `test` under that environment, with a deadline that kills the whole process group and bounded output |
| [`sqlschema`](sqlschema) | reads DDL statement by statement and applies migrations in version order (golang-migrate, goose, sql-migrate, dbmate) |
| [`sqlstmt`](sqlstmt) | reads INSERT/UPDATE/DELETE/SELECT: table, columns, which placeholder feeds which column, WHERE columns, upserts, row locks |
| [`naming`](naming) | every rule that decides something from a name, matched by whole words |
| [`cite`](cite) | checks a model's `file:line` citations: inside the repository, the line exists, the quoted code is there |
| [`llmout`](llmout) | decodes the JSON a model returned: strips markdown fences, refuses trailing prose, unknown fields (in strict mode) and runaway size |

Supported out of the box: `database/sql`, pgx, lib/pq, sqlx, gorm, bun, go-pg,
ent and the usual drivers; `net/http`, gRPC, resty and payment SDKs; Kafka
(segmentio, sarama, franz-go, confluent), NATS, RabbitMQ, SQS/SNS, Pub/Sub,
watermill; Redis and memcached. The full list is in
[`scan/iotables.go`](scan/iotables.go) — check a new repository's `go.mod`
against it first: a missing library makes its calls invisible.

## Safety

gofacts reads code; it never builds or runs it. Loading uses `go list` and the
type checker under an environment decided by `goenv`, not inherited from the
caller:

- `GOFLAGS` is replaced, so `-toolexec` or a global `-mod=vendor` cannot reach the go command
- `GOTOOLCHAIN=local`, so a `toolchain` line in `go.mod` cannot download and run another Go
- `CGO_ENABLED=0`, so the C compiler never runs with flags the repository chose
- `GOWORK` honours only a `go.work` committed at the module root
- `-mod=readonly` (or `-mod=vendor` when the repository vendors), so analysis never edits `go.mod`

`gocmd` is different: `go test` executes the repository's code. It bounds time
and output, but it is not a sandbox, and whether to run untrusted code is the
caller's decision.

Paths in model output are treated as input: `cite` refuses absolute paths,
`..` and symlinks that lead out of the repository.

## Limits

- Static analysis only. Reflection, `unsafe`, code generated at build time and
  anything another service does are invisible. A missing edge is not proof of
  absence.
- VTA looks at the module's own functions. An interface value that reaches the
  module only through library code is not seen, so such a call site can come
  back narrower than it really is.
- SQL is read by pattern. Statements assembled at run time, or written in an
  ORM DSL beyond `Model`/`Table`/`Set`/`OnConflict`, are not seen.
- cgo files are not analysed.
- A transaction is followed within one function. One begun in a function and
  committed in another is reported as escaping, and its leaks are not judged.

## Development

```bash
go test ./...
```

Dependencies are vendored, so this works with `GOFLAGS=-mod=vendor` set
globally. After changing `go.mod`:

```bash
GOFLAGS=-mod=mod go mod tidy && GOFLAGS=-mod=mod go mod vendor
```

Tests load small modules written to a temporary directory, plus the fixture
services in [`scan/testdata`](scan/testdata). Everything runs offline.

## Coming from testigo

gofacts started as testigo's `internal/scanningFlow`. What changed, what stayed
in testigo, and how testigo switches over: [TESTIGO.md](TESTIGO.md).
