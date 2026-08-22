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
