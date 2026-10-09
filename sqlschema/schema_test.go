package sqlschema

import (
	"slices"
	"testing"

	"github.com/katayunak/1819/gofacts/fact"
)

func schemaOf(t *testing.T, sql string) fact.Infra {
	t.Helper()
	var s Schema
	s.ApplySQL(sql, "m.sql")
	var infra fact.Infra
	s.Fill(&infra)
	return infra
}

func mustUnique(t *testing.T, infra fact.Infra, table, column string) fact.Coverage {
	t.Helper()
	cov, ok := infra.UniqueOn(table, column)
	if !ok {
		t.Fatalf("%s.%s should be unique; constraints: %+v", table, column, infra.Constraints)
	}
	return cov
}

func mustNotUnique(t *testing.T, infra fact.Infra, table, column string) {
	t.Helper()
	if cov, ok := infra.UniqueOn(table, column); ok {
		t.Fatalf("%s.%s should not be unique, but %+v covers it", table, column, cov.Constraint)
	}
}

func TestColumnLevelUniqueIsAConstraint(t *testing.T) {
	infra := schemaOf(t, `
CREATE TABLE payments (
    id              uuid PRIMARY KEY,
    idempotency_key text NOT NULL UNIQUE,
    amount          bigint NOT NULL
);`)
	mustUnique(t, infra, "payments", "idempotency_key")
	mustUnique(t, infra, "payments", "id")
	mustNotUnique(t, infra, "payments", "amount")

	cols := infra.ColumnsOf("payments")
	if len(cols) != 3 || !cols[0].PrimaryKey || !cols[1].NotNull || cols[1].Type != "text" {
		t.Errorf("columns = %+v", cols)
	}
}

func TestCommentedOutConstraintsDoNotCount(t *testing.T) {
	infra := schemaOf(t, `
CREATE TABLE payments (
    id       text NOT NULL,
    idem_key text NOT NULL -- UNIQUE would go here
    /* , UNIQUE (idem_key) */
);
-- CREATE UNIQUE INDEX payments_idem ON payments (idem_key);
`)
	mustNotUnique(t, infra, "payments", "idem_key")
	if !infra.HasColumn("payments", "idem_key") {
		t.Error("the table itself must still be read")
	}
}

func TestStringsThatLookLikeCommentsAreNotComments(t *testing.T) {
	infra := schemaOf(t, `CREATE TABLE notes (body text DEFAULT '-- not a comment', ref text UNIQUE);`)
	mustUnique(t, infra, "notes", "ref")
}

func TestSingleLineCreateTable(t *testing.T) {
	infra := schemaOf(t, `CREATE TABLE IF NOT EXISTS refunds (id bigserial PRIMARY KEY, payment_id text NOT NULL, UNIQUE (payment_id));`)
	mustUnique(t, infra, "refunds", "payment_id")
}

func TestUniqueIndexWithAccessMethodAndPredicate(t *testing.T) {
	infra := schemaOf(t, `
CREATE TABLE payments (id text, idem_key text, deleted_at timestamptz);
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS payments_idem_key_idx
    ON public.payments USING btree (idem_key) WHERE deleted_at IS NULL;
`)
	cov := mustUnique(t, infra, "payments", "idem_key")
	if !cov.Partial || cov.Constraint.Where != "deleted_at IS NULL" {
		t.Errorf("a partial index only guarantees uniqueness for rows that match it: %+v", cov)
	}
	if cov.Constraint.Name != "payments_idem_key_idx" || cov.Constraint.Kind != fact.KindUniqueIndex {
		t.Errorf("constraint = %+v", cov.Constraint)
	}
}

func TestCompositeUniqueScopesTheKey(t *testing.T) {
	infra := schemaOf(t, `
CREATE TABLE logs (ledger varchar NOT NULL, idempotency_key varchar, id bigint);
CREATE UNIQUE INDEX logs_idempotency_key ON logs (ledger, idempotency_key);
`)
	cov := mustUnique(t, infra, "logs", "idempotency_key")
	if !slices.Equal(cov.Scope, []string{"ledger"}) {
		t.Errorf("scope = %v, want [ledger]: the key is unique per ledger, which is how formance scopes it", cov.Scope)
	}
	if _, leads := infra.CoversColumn("logs", "idempotency_key"); leads {
		t.Error("CoversColumn keeps its leading-column meaning")
	}
}

func TestAlterTableAddUniqueWithoutAName(t *testing.T) {
	infra := schemaOf(t, `
CREATE TABLE transfers (id text, ref text, tenant text);
ALTER TABLE ONLY transfers ADD UNIQUE (ref);
ALTER TABLE IF EXISTS transfers ADD CONSTRAINT transfers_tenant_ref UNIQUE (tenant, ref);
`)
	cov := mustUnique(t, infra, "transfers", "ref")
	if len(cov.Scope) != 0 {
		t.Errorf("the single-column constraint is the stronger guarantee: %+v", cov)
	}
}

func TestLaterMigrationsDropWhatEarlierOnesCreated(t *testing.T) {
	var s Schema
	s.ApplySQL(`CREATE TABLE payments (id text PRIMARY KEY, idem_key text, ref text UNIQUE, legacy text);
CREATE UNIQUE INDEX payments_idem ON payments (idem_key);
CREATE UNIQUE INDEX payments_legacy ON payments (legacy);`, "0001.sql")
	s.ApplySQL(`DROP INDEX IF EXISTS payments_idem;
ALTER TABLE payments DROP CONSTRAINT payments_ref_key;
ALTER TABLE payments DROP COLUMN legacy;`, "0002.sql")
	var infra fact.Infra
	s.Fill(&infra)

	mustNotUnique(t, infra, "payments", "idem_key")
	mustNotUnique(t, infra, "payments", "ref")
	mustNotUnique(t, infra, "payments", "legacy")
	if infra.HasColumn("payments", "legacy") {
		t.Error("dropped column is still listed")
	}
	mustUnique(t, infra, "payments", "id")
}

func TestAColumnNamedKeyIsAColumn(t *testing.T) {
	infra := schemaOf(t, "CREATE TABLE settings (key varchar(64) NOT NULL UNIQUE, index int, value text);")
	mustUnique(t, infra, "settings", "key")
	if len(infra.ColumnsOf("settings")) != 3 {
		t.Errorf("columns = %+v", infra.ColumnsOf("settings"))
	}
}

func TestRenamesFollowTheColumn(t *testing.T) {
	infra := schemaOf(t, `
CREATE TABLE payments (id text, key text UNIQUE);
ALTER TABLE payments RENAME COLUMN key TO idem_key;
ALTER TABLE payments RENAME TO charges;
`)
	mustUnique(t, infra, "charges", "idem_key")
	if infra.HasTable("payments") {
		t.Error("renamed table is still listed under its old name")
	}
}

func TestAddColumnWithInlineUnique(t *testing.T) {
	infra := schemaOf(t, `
CREATE TABLE payments (id text);
ALTER TABLE payments ADD COLUMN IF NOT EXISTS idem_key text NOT NULL UNIQUE, ADD COLUMN note text;
ALTER TABLE payments ALTER COLUMN note SET NOT NULL;
`)
	mustUnique(t, infra, "payments", "idem_key")
	cols := infra.ColumnsOf("payments")
	if len(cols) != 3 || !cols[2].NotNull {
		t.Errorf("columns = %+v", cols)
	}
}

func TestMySQLBackticksAndUniqueKey(t *testing.T) {
	infra := schemaOf(t, "CREATE TABLE `orders` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `order_ref` varchar(64) NOT NULL,\n  PRIMARY KEY (`id`),\n  UNIQUE KEY `uk_order_ref` (`order_ref`),\n  KEY `idx_created` (`id`)\n) ENGINE=InnoDB;")
	cov := mustUnique(t, infra, "orders", "order_ref")
	if cov.Constraint.Name != "uk_order_ref" {
		t.Errorf("name = %q", cov.Constraint.Name)
	}
}

func TestForeignKeysChecksAndEnums(t *testing.T) {
	infra := schemaOf(t, `
CREATE TYPE payment_status AS ENUM ('pending', 'captured');
CREATE TABLE payments (
    id text PRIMARY KEY,
    order_id text REFERENCES orders(id) ON DELETE CASCADE,
    status text CHECK (status IN ('pending', 'captured', 'failed')),
    CONSTRAINT fk_wallet FOREIGN KEY (wallet_id) REFERENCES wallets (id)
);`)
	if len(infra.ForeignKeys) != 2 {
		t.Fatalf("foreign keys = %+v", infra.ForeignKeys)
	}
	if fk := infra.ForeignKeys[0]; fk.RefTable != "orders" || fk.OnDelete != "CASCADE" || !slices.Equal(fk.RefColumns, []string{"id"}) {
		t.Errorf("inline fk = %+v", fk)
	}
	var enum, check *fact.Check
	for i := range infra.Checks {
		switch infra.Checks[i].Expr {
		case "enum type payment_status":
			enum = &infra.Checks[i]
		default:
			check = &infra.Checks[i]
		}
	}
	if enum == nil || !slices.Equal(enum.Values, []string{"pending", "captured"}) {
		t.Errorf("enum = %+v", enum)
	}
	if check == nil || !slices.Equal(check.Values, []string{"pending", "captured", "failed"}) {
		t.Errorf("check = %+v", check)
	}
}

func TestLineNumbersPointAtTheStatement(t *testing.T) {
	sts := Parse("-- header\n\nCREATE TABLE a (id int);\n\n\nCREATE UNIQUE INDEX a_id ON a (id);\n", "m.sql")
	if len(sts) != 2 || sts[0].Line != 3 || sts[1].Line != 6 {
		t.Fatalf("statements = %+v", sts)
	}
}

func TestDollarQuotedBodiesAreNotSplit(t *testing.T) {
	sts := Parse(`CREATE FUNCTION f() RETURNS trigger AS $$ BEGIN NEW.x := 1; RETURN NEW; END; $$ LANGUAGE plpgsql;
CREATE TABLE t (id int UNIQUE);`, "m.sql")
	if len(sts) != 2 || sts[0].Kind != KindOther || sts[1].Kind != KindCreateTable {
		t.Fatalf("statements = %+v", sts)
	}
}

func TestTestigoFixturesStillParse(t *testing.T) {
	infra := schemaOf(t, `CREATE TABLE IF NOT EXISTS payments (
    id          TEXT NOT NULL,
    idem_key    TEXT NOT NULL,
    cents       BIGINT NOT NULL,
    status      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id       BIGSERIAL,
    account  TEXT NOT NULL,
    cents    BIGINT NOT NULL,
    PRIMARY KEY (id)
);`)
	if len(infra.Tables) != 2 || len(infra.ColumnsOf("payments")) != 5 {
		t.Fatalf("tables = %+v", infra.Tables)
	}
	mustUnique(t, infra, "payments", "id")
	mustNotUnique(t, infra, "payments", "idem_key")
	if c := infra.ColumnsOf("payments")[4]; c.Default != "now()" {
		t.Errorf("default = %q", c.Default)
	}
}
