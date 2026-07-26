package migrations

import (
	"strings"
	"testing"
)

func TestBookSeriesKeysMigration(t *testing.T) {
	t.Parallel()
	b, err := FS.ReadFile("sql/20260726130000_book_series_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(b)
	for _, want := range []string{
		"ALTER TABLE audiobook_series ADD COLUMN series_key",
		"ALTER TABLE ebook_series ADD COLUMN series_key",
		"DELETE FROM audiobook_series WHERE series_key = ''",
		"DELETE FROM ebook_series WHERE series_key = ''",
		"ADD CONSTRAINT audiobook_series_key_not_empty CHECK (series_key <> '')",
		"ADD CONSTRAINT ebook_series_key_not_empty CHECK (series_key <> '')",
		"CREATE INDEX audiobook_series_key_index",
		"CREATE INDEX ebook_series_key_index",
		"DROP CONSTRAINT IF EXISTS ebook_series_key_not_empty",
		"DROP CONSTRAINT IF EXISTS audiobook_series_key_not_empty",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
