package bookmeta

import (
	"strings"
	"unicode"
)

// TrustedAutomaticCredit reports whether scanner-derived author or narrator
// metadata is safe to persist. It accepts names from every script, including
// initials and single-letter names, while rejecting common identifier and
// producer-site artifacts.
func TrustedAutomaticCredit(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || looksLikeCreditURL(value) || looksLikeISBNCredit(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func looksLikeISBNCredit(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "isbn") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(value, "isbn"), "-: "))
	if rest == "" {
		return true
	}
	for _, r := range rest {
		if unicode.IsDigit(r) || r == 'x' || r == '-' || unicode.IsSpace(r) {
			continue
		}
		return false
	}
	return true
}

func looksLikeCreditURL(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "www.") || strings.Contains(lower, "@") {
		return true
	}
	if strings.ContainsAny(value, " \t\r\n/") {
		return false
	}
	labels := strings.Split(strings.Trim(value, "."), ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" {
			return false
		}
		for _, r := range label {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
				continue
			}
			return false
		}
	}
	tld := strings.ToLower(labels[len(labels)-1])
	if len(tld) < 2 || len(tld) > 24 {
		return false
	}
	for _, r := range tld {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	_, common := map[string]struct{}{
		"com": {}, "org": {}, "net": {}, "io": {}, "co": {}, "de": {},
		"uk": {}, "us": {}, "info": {}, "biz": {}, "me": {}, "xyz": {},
	}[tld]
	return common || (value == lower && len(labels) >= 3)
}
