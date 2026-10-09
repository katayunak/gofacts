package sqlstmt

import (
	"maps"
	"slices"
	"testing"
)

func one(t *testing.T, sql string) Statement {
	t.Helper()
	sts := Parse(sql)
	if len(sts) != 1 {
		t.Fatalf("statements = %+v", sts)
	}
	return sts[0]
}

func TestInsertBindsColumnsToPlaceholders(t *testing.T) {
	st := one(t, "insert into payments(id, idem_key, cents, status) values($1,$2,$3,$4)")
	if st.Kind != Insert || st.Table != "payments" {
		t.Fatalf("st = %+v", st)
	}
	want := map[string]int{"id": 1, "idem_key": 2, "cents": 3, "status": 4}
	if !maps.Equal(st.Binds, want) {
		t.Errorf("binds = %v, want %v", st.Binds, want)
	}
}

func TestQuestionMarksAreNumberedByPosition(t *testing.T) {
	st := one(t, "INSERT INTO `transactions` (id, wallet_id, note) VALUES (?, ?, 'what?')")
	want := map[string]int{"id": 1, "wallet_id": 2}
	if !maps.Equal(st.Binds, want) || st.Placeholders != 2 {
		t.Errorf("binds = %v (%d placeholders); a ? inside a literal is not a placeholder", st.Binds, st.Placeholders)
	}
}

func TestUpsertAndConflictTarget(t *testing.T) {
	st := one(t, `INSERT INTO payments (id, idem_key) VALUES ($1, $2) ON CONFLICT (idem_key) DO NOTHING`)
	if !st.Upsert || !slices.Equal(st.ConflictColumns, []string{"idem_key"}) {
		t.Errorf("st = %+v", st)
	}
	if !one(t, "INSERT INTO t (a) VALUES (?) ON DUPLICATE KEY UPDATE a = VALUES(a)").Upsert {
		t.Error("MySQL upsert")
	}
}

func TestUpdateSetAndWhere(t *testing.T) {
	st := one(t, "UPDATE wallets SET balance = $1, status = 'active' WHERE id = $2 AND tenant_id = $3")
	if st.Kind != Update || st.Table != "wallets" {
		t.Fatalf("st = %+v", st)
	}
	if !slices.Equal(st.Columns, []string{"balance", "status"}) || st.Binds["balance"] != 1 {
		t.Errorf("set = %v %v", st.Columns, st.Binds)
	}
	if len(st.SelfArith) != 0 {
		t.Error("balance = $1 is computed in Go and written back: a lost-update shape, not in-database math")
	}
	if !slices.Equal(st.Where, []string{"id", "tenant_id"}) || st.WhereBinds["id"] != 2 || st.WhereBinds["tenant_id"] != 3 {
		t.Errorf("where = %v %v", st.Where, st.WhereBinds)
	}
}

func TestInDatabaseArithmetic(t *testing.T) {
	st := one(t, `UPDATE accounts AS a SET balance = balance - $1, version = a.version + 1 WHERE id = $2`)
	if !slices.Equal(st.SelfArith, []string{"balance", "version"}) {
		t.Errorf("self arith = %v; an alias-qualified column is still the same column", st.SelfArith)
	}
}

func TestSelectWhereAndLocks(t *testing.T) {
	st := one(t, "select count(*) from payments where idem_key=$1 for update")
	if st.Kind != Select || st.Table != "payments" || !st.Locks {
		t.Fatalf("st = %+v", st)
	}
	if !slices.Equal(st.Where, []string{"idem_key"}) || st.WhereBinds["idem_key"] != 1 {
		t.Errorf("where = %v %v", st.Where, st.WhereBinds)
	}
}

func TestDeleteAndCTE(t *testing.T) {
	if st := one(t, "DELETE FROM sessions WHERE expires_at < now()"); st.Kind != Delete || st.Table != "sessions" {
		t.Errorf("st = %+v", st)
	}
	st := one(t, "WITH moved AS (SELECT id FROM a) UPDATE b SET x = $1 WHERE id = $2")
	if st.Kind != Update || st.Table != "b" {
		t.Errorf("cte update = %+v", st)
	}
}

func TestLooksFiltersOrdinaryStrings(t *testing.T) {
	for _, s := range []string{"insert into t values (1)", "  SELECT 1", "-- c\nUPDATE t SET a=1"} {
		if !Looks(s) {
			t.Errorf("%q is SQL", s)
		}
	}
	for _, s := range []string{"Idempotency-Key", "update failed", "selection"} {
		if Looks(s) {
			t.Errorf("%q is not SQL", s)
		}
	}
}
