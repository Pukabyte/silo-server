package bookmeta

import "testing"

func TestTrustedAutomaticCredit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{".", false},
		{"#233", false},
		{"0012163", false},
		{"ISBN 978-0-306-40615-7", false},
		{"de.downmagaz.com", false},
		{"Example.COM", false},
		{"https://example.com/books", false},
		{"www.example.org", false},
		{"books@example.org", false},
		{"\uFFFD\u00ad\u00d9", true},
		{"Ada\x00Writer", false},
		{"Q", true},
		{"R.", true},
		{"A. F. Carter", true},
		{"李白", true},
		{"Élodie", true},
		{"Will.I.Am", true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := TrustedAutomaticCredit(tc.name); got != tc.want {
				t.Fatalf("TrustedAutomaticCredit(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
