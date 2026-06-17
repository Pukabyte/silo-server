package catalog

import (
	"strings"
	"testing"
)

func TestBuildAudiobookGroupsSQL_UsesItemFileStatsInsteadOfMediaFilesSubquery(t *testing.T) {
	plan, err := buildAudiobookGroupsSQL(AudiobookGroupsQuery{
		LibraryID:    10,
		GroupBy:      AudiobookGroupByAuthor,
		Sort:         "count",
		Limit:        60,
		Offset:       0,
		IncludeTotal: true,
	}, AccessFilter{})
	if err != nil {
		t.Fatalf("buildAudiobookGroupsSQL error: %v", err)
	}

	if !strings.Contains(plan.SQL, "audiobook_item_file_stats") {
		t.Fatalf("audiobook groups query must use maintained item file stats; got:\n%s", plan.SQL)
	}
	if strings.Contains(plan.SQL, "FROM media_files mf") || strings.Contains(plan.SQL, "SELECT SUM(mf.duration)") {
		t.Fatalf("audiobook groups query must not use the old correlated media_files duration lookup; got:\n%s", plan.SQL)
	}
}

func TestBuildAudiobookGroupsSQL_SkipTotalOmitsWindowCountAndFetchesLimitPlusOne(t *testing.T) {
	plan, err := buildAudiobookGroupsSQL(AudiobookGroupsQuery{
		LibraryID:    10,
		GroupBy:      AudiobookGroupByNarrator,
		Sort:         "name",
		Limit:        60,
		Offset:       120,
		IncludeTotal: false,
	}, AccessFilter{})
	if err != nil {
		t.Fatalf("buildAudiobookGroupsSQL error: %v", err)
	}

	if strings.Contains(plan.SQL, "COUNT(*) OVER ()") {
		t.Fatalf("include_total=false must omit window counts; got:\n%s", plan.SQL)
	}
	if strings.Contains(plan.SQL, "total_groups") {
		t.Fatalf("include_total=false must omit exact total selection; got:\n%s", plan.SQL)
	}
	if plan.FetchLimit != 61 {
		t.Fatalf("FetchLimit = %d, want limit+1 for has_more detection", plan.FetchLimit)
	}
	if plan.Limit != 60 {
		t.Fatalf("Limit = %d, want requested page size", plan.Limit)
	}
}

func TestBuildAudiobookGroupsSQL_SearchAppliesNormalizedPrefixFilter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		groupBy AudiobookGroupBy
		want    string
	}{
		{name: "author", groupBy: AudiobookGroupByAuthor, want: "LOWER(BTRIM(p.name)) LIKE $"},
		{name: "narrator", groupBy: AudiobookGroupByNarrator, want: "LOWER(BTRIM(p.name)) LIKE $"},
		{name: "series", groupBy: AudiobookGroupBySeries, want: "LOWER(BTRIM(s.series_name)) LIKE $"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := buildAudiobookGroupsSQL(AudiobookGroupsQuery{
				LibraryID:    10,
				GroupBy:      tc.groupBy,
				Sort:         "name",
				Limit:        60,
				SearchPrefix: "Neil",
				IncludeTotal: true,
			}, AccessFilter{})
			if err != nil {
				t.Fatalf("buildAudiobookGroupsSQL error: %v", err)
			}

			if !strings.Contains(plan.SQL, tc.want) {
				t.Fatalf("expected normalized prefix predicate %q; got:\n%s", tc.want, plan.SQL)
			}
			if !containsArg(plan.Args, "neil%") {
				t.Fatalf("search args = %#v, want neil%%", plan.Args)
			}
		})
	}
}

func containsArg(args []any, want any) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
