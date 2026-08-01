package catalog

import (
	"context"
	"strings"
	"testing"
)

func TestSearchDistinctBookSeriesRejectsEmptyNormalizedPrefix(t *testing.T) {
	t.Parallel()
	values, hasMore, err := searchDistinctAudiobookSeriesWithSource(
		context.Background(), nil, BrowseFilters{}, "media_items mi", "ebook", " -- ", 10,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 || hasMore {
		t.Fatalf("values=%v hasMore=%v, want empty", values, hasMore)
	}
}

func TestBuildBookSeriesFacetSearchSQLUsesCanonicalKey(t *testing.T) {
	t.Parallel()
	query := buildBookSeriesFacetSearchSQL("media_items mi", "ebook_series", "WHERE TRUE", 1, 10)
	for _, want := range []string{"JOIN ebook_series", "s.series_key LIKE $1", "s.series_key <> ''", "GROUP BY s.series_key"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "LOWER(BTRIM(s.series_name)) LIKE") {
		t.Fatalf("query must not search display name identity:\n%s", query)
	}
}
