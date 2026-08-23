package sqlite

import "testing"

func TestMigrationIdentityIncludesSQLChecksum(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	migration := Migration{Version: 1, Name: "create example", SQL: "CREATE TABLE example (id INTEGER PRIMARY KEY)"}
	applied, err := db.ApplyMigration(migration)
	if err != nil || !applied {
		t.Fatalf("expected first application, got applied=%v err=%v", applied, err)
	}
	applied, err = db.ApplyMigration(migration)
	if err != nil || applied {
		t.Fatalf("expected matching migration to be skipped, got applied=%v err=%v", applied, err)
	}
	migration.SQL = "CREATE TABLE example (id INTEGER PRIMARY KEY, name TEXT)"
	if _, err := db.ApplyMigration(migration); err == nil {
		t.Fatal("expected changed migration SQL to be rejected")
	}
}

func TestFailedMigrationCanBeCorrectedAndRetried(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	broken := Migration{
		Version: 1,
		Name:    "retry migration",
		SQL:     "CREATE TABLE partial_table (id INTEGER); INVALID SQL;",
	}
	if _, err := db.ApplyMigration(broken); err == nil {
		t.Fatal("expected first migration attempt to fail")
	}
	corrected := Migration{
		Version: 1,
		Name:    "retry migration",
		SQL:     "CREATE TABLE recovered_table (id INTEGER PRIMARY KEY)",
	}
	applied, err := db.ApplyMigration(corrected)
	if err != nil || !applied {
		t.Fatalf("corrected migration was not retryable: applied=%v err=%v", applied, err)
	}
	version, err := db.CurrentVersion()
	if err != nil || version != 1 {
		t.Fatalf("expected recovered version 1, got version=%d err=%v", version, err)
	}
	partial, err := db.TableExists("partial_table")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := db.TableExists("recovered_table")
	if err != nil {
		t.Fatal(err)
	}
	if partial || !recovered {
		t.Fatalf("unexpected recovered schema: partial=%v recovered=%v", partial, recovered)
	}
}

func TestFailedMigrationRollsBackSchemaAndHistory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	applied, err := db.ApplyMigration(Migration{
		Version: 1,
		Name:    "broken",
		SQL: `
			CREATE TABLE should_rollback (id INTEGER PRIMARY KEY);
			THIS IS NOT VALID SQL;
		`,
	})
	if err == nil || applied {
		t.Fatalf("expected unapplied migration error, got applied=%v err=%v", applied, err)
	}

	version, err := db.CurrentVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Fatalf("expected version 0, got %d", version)
	}
	exists, err := db.TableExists("should_rollback")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected migration schema changes to roll back")
	}
}
