package abs

import "testing"

func TestEbookContentType(t *testing.T) {
	tests := map[string]string{
		".epub": "application/epub+zip",
		".PDF":  "application/pdf",
		".cbz":  "application/vnd.comicbook+zip",
		".m4b":  "",
	}
	for ext, want := range tests {
		if got := ebookContentType(ext); got != want {
			t.Errorf("ebookContentType(%q) = %q, want %q", ext, got, want)
		}
	}
}
