package sqlschema

import (
	"strings"
	"testing"
)

func TestGolangMigrateDownFilesAreNotSchema(t *testing.T) {
	up := SplitMigration("db/0002_idem.up.sql", "CREATE UNIQUE INDEX p_idem ON payments (idem_key);")
	down := SplitMigration("db/0002_idem.down.sql", "DROP INDEX p_idem;")
	if up.Tool != ToolGolangMigrate || !up.HasUp || up.HasDown || up.Version != "0002" {
		t.Errorf("up = %+v", up)
	}
	if down.HasUp || !down.HasDown || down.Up != "" {
		t.Errorf("a down file contributes nothing to the schema: %+v", down)
	}
	tool, total, withDown := DownCoverage([]Migration{up, down, SplitMigration("db/0003_x.up.sql", "SELECT 1;")})
	if tool != ToolGolangMigrate || total != 2 || withDown != 1 {
		t.Errorf("coverage = %s %d/%d, want golang-migrate 1/2", tool, withDown, total)
	}
}

func TestGooseDownSectionIsBlankedButLinesSurvive(t *testing.T) {
	src := "-- +goose Up\nCREATE TABLE a (id int);\nCREATE UNIQUE INDEX a_id ON a (id);\n\n-- +goose Down\nDROP TABLE a;\n"
	m := SplitMigration("migrations/20240101_a.sql", src)
	if m.Tool != ToolGoose || !m.HasDown {
		t.Fatalf("m = %+v", m)
	}
	if strings.Contains(m.Up, "DROP TABLE") {
		t.Fatal("the down section leaked into the schema: applying it would drop the table the up section created")
	}
	sts := Parse(m.Up, m.Path)
	if len(sts) != 2 || sts[1].Line != 3 {
		t.Fatalf("statements = %+v", sts)
	}
}

func TestEmptyDownSectionIsNoDown(t *testing.T) {
	m := SplitMigration("m/1_a.sql", "-- migrate:up\nCREATE TABLE a (id int);\n-- migrate:down\n-- nothing\n")
	if m.Tool != ToolDbmate || m.HasDown {
		t.Fatalf("a down section holding only a comment cannot roll anything back: %+v", m)
	}
}

func TestSQLMigrateMarkers(t *testing.T) {
	m := SplitMigration("m/2_b.sql", "-- +migrate Up\nCREATE TABLE b (id int);\n-- +migrate Down\nDROP TABLE b;\n")
	if m.Tool != ToolSQLMigrate || !m.HasDown || strings.Contains(m.Up, "DROP") {
		t.Fatalf("m = %+v", m)
	}
}

func TestPlainSQLIsNotCountedForDownCoverage(t *testing.T) {
	_, total, _ := DownCoverage([]Migration{SplitMigration("schema.sql", "CREATE TABLE a (id int);")})
	if total != 0 {
		t.Fatal("a plain schema file has no down convention; counting it as missing one is a false finding")
	}
}

func TestOrderIsNumericNotLexical(t *testing.T) {
	ms := []Migration{
		SplitMigration("m/10_c.up.sql", ""),
		SplitMigration("m/9_b.up.sql", ""),
		SplitMigration("m/schema.sql", ""),
		SplitMigration("m/1_a.up.sql", ""),
	}
	Order(ms)
	var got []string
	for _, m := range ms {
		got = append(got, m.Path)
	}
	want := "m/1_a.up.sql m/9_b.up.sql m/10_c.up.sql m/schema.sql"
	if strings.Join(got, " ") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}
