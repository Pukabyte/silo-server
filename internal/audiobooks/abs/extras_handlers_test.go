package abs

import (
	"testing"

	"github.com/Silo-Server/silo-server/internal/models"
)

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

func TestSelectEbookFile(t *testing.T) {
	files := []*models.MediaFile{
		{ID: 1, FilePath: "/books/supplement.pdf"},
		{ID: 2, FilePath: "/books/primary.epub"},
		{ID: 3, FilePath: "/books/audio.m4b"},
	}
	if got := selectEbookFile(files, 0); got == nil || got.ID != 2 {
		t.Fatalf("primary ebook = %#v, want EPUB id 2", got)
	}
	if got := selectEbookFile(files, 1); got == nil || got.ID != 1 {
		t.Fatalf("requested ebook = %#v, want PDF id 1", got)
	}
	if got := selectEbookFile(files, 3); got != nil {
		t.Fatalf("audio file selected as ebook: %#v", got)
	}
}
