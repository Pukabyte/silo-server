package bookmeta

import "testing"

func TestNormalizeSeriesKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		want string
	}{
		{"Star Wars: Legends;", "starwarslegends"},
		{"Star-Wars Legends", "starwarslegends"},
		{"  Psy-Changeling  ", "psychangeling"},
		{"Ведьмак № 2", "ведьмак2"},
	} {
		if got := NormalizeSeriesKey(tc.name); got != tc.want {
			t.Errorf("NormalizeSeriesKey(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCleanSeriesDisplay(t *testing.T) {
	t.Parallel()
	if got := CleanSeriesDisplay("  Star   Wars: Legends; "); got != "Star Wars: Legends" {
		t.Fatalf("CleanSeriesDisplay = %q", got)
	}
}
