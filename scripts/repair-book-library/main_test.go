package main

import "testing"

func TestValidAutomaticCredit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		want bool
	}{
		{".", false},
		{"9780719824029", false},
		{"#233", false},
		{"ISBN 978-0-306-40615-7", false},
		{"de.downmagaz.com", false},
		{"https://example.com/books", false},
		{"A", true},
		{"R.", true},
		{"Ursula K. Le Guin", true},
		{"刘慈欣", true},
	} {
		if got := validAutomaticCredit(tc.name); got != tc.want {
			t.Errorf("validAutomaticCredit(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSuspiciousEbookTitle(t *testing.T) {
	t.Parallel()
	for _, title := range []string{"Cover", " contents ", "de.downmagaz.com"} {
		if !isSuspiciousEbookTitle(title) {
			t.Errorf("isSuspiciousEbookTitle(%q) = false", title)
		}
	}
	if isSuspiciousEbookTitle("The Left Hand of Darkness") {
		t.Fatal("substantive title classified as suspicious")
	}
}

func TestRequiresEbookTitleRepair(t *testing.T) {
	for _, title := range []string{"\u00b4", "275736108", "garbled\x1btitle"} {
		if !requiresEbookTitleRepair(title) {
			t.Errorf("requiresEbookTitleRepair(%q) = false", title)
		}
	}
	if requiresEbookTitleRepair("A readable\nsubtitle") {
		t.Fatal("readable multiline title requires repair")
	}
}
