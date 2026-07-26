package bookmeta

import (
	"strings"
	"unicode"
)

// NormalizeSeriesKey returns a punctuation-, spacing-, and case-insensitive
// identity key while preserving letters and digits from every script.
func NormalizeSeriesKey(value string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

// CleanSeriesDisplay removes scanner-noise separators without rewriting the
// human-facing spelling of a series name.
func CleanSeriesDisplay(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return strings.TrimSpace(strings.TrimRight(value, ";|"))
}
