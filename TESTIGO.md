# gofacts and testigo

gofacts is testigo's `internal/scanningFlow` and `flowEntity`, extracted so
testigo and the 1819 reviewer share one implementation. testigo's own scanner
tests are ported in [`scan/parity_test.go`](scan/parity_test.go) and pass.

## What changed

Each row has a test in [`scan/improvements_test.go`](scan/improvements_test.go)
or in the package's own tests.

| testigo did | now | test |
|---|---|---|
| inherited the caller's env: a global `GOFLAGS=-mod=vendor` broke every non-vendored repo; `-toolexec`, `GOTOOLCHAIN=auto` and cgo were live | env decided by `goenv` | `TestInheritedFlagsCannotReachTheGoCommand`, `TestLoadsDespiteAHostileGlobalGOFLAGS` |
| pruned CHA edges by import closure, dropping implementations behind an interface declared where it is consumed (the usual layout) | VTA narrows sites the module wires; CHA is the labelled fallback | `TestImplementationBehindAConsumerInterfaceIsReached`, `TestWithoutWiringEveryImplementationIsAPossibleCallee` |
| transactions were three booleans per function | SSA control-flow window from begin to commit/rollback, error branch of the begin excluded | `TestNetworkCallAfterCommitIsNotInsideTheTransaction`, `TestAReturnBetweenBeginAndCommitIsALeak` |
| a network call one helper away was invisible to `TX-NET-CALL` | module callees judged by what they reach | `TestNetworkCallThroughAHelperIsInsideTheTransaction` |
| — | `TX-UNUSED`: a transaction nothing runs on (walletsvc's `Atomic`) | `TestATransactionNobodyUsesProtectsNothing` |
| — | `TX-BYPASS`: database work beside the open transaction, incl. gorm's `db.Create` inside `db.Transaction` | `TestDatabaseWorkBesideTheTransactionIsReported`, `TestGormCallbackWritingThroughTheOuterHandle`, `TestLedgerPostOutsideThePaymentTransaction` |
| SQL read from the first argument only, so every `ExecContext(ctx, sql)` and pgx call was missed | any argument, constants and single-assignment locals | `TestSQLAfterTheContextArgumentIsRead`, `TestSQLInConstantsAndLocalsIsRead` |
| ORM write verbs matched by name on any receiver | only on database-library methods | — |
| column-level `UNIQUE`/`PRIMARY KEY`, `USING btree`, single-line `CREATE TABLE`, `ALTER TABLE ONLY ... ADD UNIQUE` unseen; comments counted | statement-scoped DDL reader | `sqlschema/schema_test.go` |
| every `.sql` read flat: down files were schema, `DROP INDEX` ignored | migrations applied in version order, down sections skipped | `TestADroppedIndexNoLongerProtectsTheKey`, `sqlschema/migration_test.go` |
| composite unique `(tenant, key)` did not protect the key (formance ledger's shape) | `Infra.UniqueOn` reports scope and partial predicates | `TestACompositeUniqueScopedByTenantSatisfiesTheKey` |
| idempotency key checked against `snake_case(field)` on any table | against the column the INSERT binds it to, on that table | `TestIdempotencyKeyIsCheckedAgainstTheColumnItIsStoredIn` |
| key evidence pooled by field name across structs | keyed by the field's `types.Var` | `TestOriginEvidenceBelongsToTheFieldNotTheName` |
| "looked up" = passed to any call named like a query, inserts included | a WHERE clause on its column, or a database call | as above |
| bun tags unread; gorm composite `uniqueIndex` split into single-column ones; `TableName()` ignored | read | `TestBunAndGormTagsDescribeTheSchema` |
| a state passed as a typed argument (`UpdateStatus(ctx, id, StatusCaptured)`) was not a write, so `STATE-NEVER-SET` lied | counted; `fmt.Println(state)` still is not | `TestStateSetThroughATypedArgumentIsProduced` |
| — | source states from guards: `if p.Status == A`, `switch`, early return | `TestGuardsGiveTheSourceState` |
| `StateWrite.InTx` = "the function opens a tx somewhere" | inside the transaction window | `TestStateWritesKnowWhetherTheTransactionIsOpen` |
| substring name tables: `GlobalWeight` was money, `StandardFee` a key, `FeeRate float64` "money in a float" | whole words; rates are not amounts | `naming/naming_test.go`, `TestMoneyIsMatchedByWordsAndRatesAreNotAmounts` |
| `LOAD-ERROR` and `STATE-NEVER-SET` had no file or line | located | `TestLoadErrorsAreFindingsWithALocation` |
| methods of generic types missing | indexed | `TestSymbolNaming` |
| goroutine hand-off only a per-function flag | marked on the edge | `TestGoroutineHandOffIsMarkedOnTheEdge` |
| answers' citations checked by regex `\.go:\d+` | `cite.Verifier`: path containment, line exists, quote matches | `cite/cite_test.go` |
| `go build/vet/test` on the repo with no deadline, inherited env, unbounded output | `gocmd` | `gocmd/gocmd_test.go` |
| `flow.json` carried a timestamp, so two scans differed | deterministic; `GeneratedAt` is the caller's to set | `TestOutputIsDeterministic` |

New, for the reviewer: `Functions`, `FunctionAt`, `Callers`, `Callees`,
`Edges`, `TestsFor`, `Transactions`, program-wide `Findings` (testigo kept
structural findings only for reachable nodes; `Flow` still does),
`StateMachine.Transitions` and `StateRoles.Illegal`.

## What stayed in testigo

The test catalogue and scenarios, the agent rounds, prompts and planner, the
markdown docs scorer (`FindDocs`) and the report renderer are testigo's
product, not shared facts. `fact.Flow` keeps a `Docs` field so a flow written
by testigo keeps its shape.

Worth sharing later, once 1819 needs them: the token cost model
(`agent/planner/cost.go`), shared-preamble hoisting (`prompts/preamble.go`),
the mermaid flow renderer (`report/mermaid.go`) and the MCP server scaffolding.

## Switching testigo over

1. `require github.com/katayunak/1819/gofacts` (with
   `replace => ../1819/gofacts` until it is tagged), then `go mod vendor`.
2. `flowEntity.X` → `fact.X`. Every identifier testigo uses exists, `StateRole`
   and `StateRoles` included. `fact.SchemaVersion` is 5; version 4 keys keep
   their names.
3. `scanningFlow.Scan(opts)` →
   `scan.Load(ctx, scan.Options{Dir: root, SkipDirs: []string{"testigo", "_to_delete"}})`,
   then `prog.Flow(entries)`. Set `flow.GeneratedAt` and `flow.Docs` as before.
4. `patterns.*` → `naming.*`; `hasCitation` → `cite.Has`, or better
   `cite.Verifier.Quote`; `stripFence` + `json.Unmarshal` → `llmout.Decode`,
   which also refuses prose after the JSON.
5. `agent.run` → `gocmd.Run`.

Expect different output, deliberately: more nodes behind consumer-defined
interfaces, fewer `TX-NET-CALL` false positives, new `TX-UNUSED` and
`TX-BYPASS` findings, table writes that used to be missing, and no
`IDEM-KEY-NOT-UNIQUE` for fields the table does not store (paysvc's `OrderID`).
