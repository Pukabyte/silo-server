package abs

import "testing"

func TestFilterMatchesGenreAndLanguageMetadata(t *testing.T) {
	item := LibraryItem{Media: LibraryItemMedia{Metadata: Metadata{
		Genres:   []string{"Science Fiction", "Adventure"},
		Language: testJapanese,
	}}}
	if !((Filter{Kind: FilterGenres, Value: "science fiction"}).Matches(item, false, false, false)) {
		t.Fatal("genre filter did not match mapped metadata")
	}
	if !((Filter{Kind: FilterLanguages, Value: "JPN"}).Matches(item, false, false, false)) {
		t.Fatal("language filter did not match mapped metadata")
	}
}
