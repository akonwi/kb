package sqlite

import "testing"

func TestFTS5BatchInsertAndRankedSearch(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	enabled, err := db.FTS5Enabled()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("SQLite build does not include FTS5")
	}
	if err := db.CreateIndex(); err != nil {
		t.Fatal(err)
	}

	documents := []Document{
		{Path: "notes/sqlite.md", Title: "SQLite migrations", Body: "Apply schema migrations inside a transaction."},
		{Path: "notes/search.md", Title: "Search", Body: "SQLite FTS5 provides full-text search."},
		{Path: "notes/ard.md", Title: "Ard", Body: "Ard compiles programs to Go."},
	}
	if err := db.InsertDocuments(documents); err != nil {
		t.Fatal(err)
	}

	results, err := db.Search("sqlite migrations", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Path != "notes/sqlite.md" {
		t.Fatalf("expected migration document first, got %q", results[0].Path)
	}
	if results[0].ID != 1 || results[0].Title != "SQLite migrations" || results[0].Rank >= 0 {
		t.Fatalf("unexpected typed result: %#v", results[0])
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
