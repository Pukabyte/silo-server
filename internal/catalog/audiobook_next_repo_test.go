package catalog

import (
	"strings"
	"testing"
)

func TestBuildAudiobookNextSeriesSQLUsesCanonicalKey(t *testing.T) {
	t.Parallel()
	query := buildAudiobookNextSeriesSQL("", 3)
	for _, want := range []string{
		"GROUP BY s.series_key",
		"s2.series_key = fs.series_key",
		"s.series_key <> ''",
		"s2.series_key <> ''",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "LOWER(BTRIM(s.series_name))") || strings.Contains(query, "LOWER(BTRIM(s2.series_name))") {
		t.Fatalf("query must not derive identity from display names:\n%s", query)
	}
}
