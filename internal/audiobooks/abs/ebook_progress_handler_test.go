package abs

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Silo-Server/silo-server/internal/models"
)

type recordingEbookProgressStore struct {
	row        *EbookProgress
	upserted   *EbookProgress
	hidden     *bool
	hiddenItem string
	committed  *EbookProgress
	rows       []EbookProgress
}

func (s *recordingEbookProgressStore) GetEbookProgress(context.Context, string, string, string) (*EbookProgress, error) {
	return s.row, nil
}

func (s *recordingEbookProgressStore) ListEbookProgress(context.Context, string, string, int) ([]EbookProgress, error) {
	return s.rows, nil
}

func (s *recordingEbookProgressStore) UpsertEbookProgress(_ context.Context, progress EbookProgress) (*EbookProgress, error) {
	s.upserted = &progress
	if s.committed != nil {
		return s.committed, nil
	}
	return &progress, nil
}

func (s *recordingEbookProgressStore) DeleteEbookProgress(context.Context, string, string, string) error {
	return nil
}

func (s *recordingEbookProgressStore) SetEbookHidden(_ context.Context, _, _, contentID string, hide bool) error {
	s.hidden = &hide
	s.hiddenItem = contentID
	return nil
}

func TestSetItemProgressRejectsOversizedBody(t *testing.T) {
	body := []byte(`{"isFinished":false}` + strings.Repeat(" ", int(maxProgressBodyBytes)))
	h := New(Dependencies{MediaStore: noopMediaStore{}})
	rec := dispatchABSWithParams(http.MethodPost, "/api/me/progress/ebook-1",
		map[string]string{"libraryItemId": testEbookID}, body, "1", testProfileID, h.handleSetItemProgress) //nolint:goconst // External ABS route key.
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetEbookProgressFinishedFlagForcesCompletion(t *testing.T) {
	store := &recordingEbookProgressStore{row: &EbookProgress{
		UserID: "1", ProfileID: testProfileID, ContentID: testEbookID, FileID: 7, Progress: 0.4,
	}}
	media := &stubMediaStore{known: map[string]*models.MediaItem{
		testEbookID: {ContentID: testEbookID, Type: mediaTypeEbook},
	}}
	h := New(Dependencies{MediaStore: media, EbookProgressStore: store})
	rec := dispatchABSWithParams(http.MethodPost, "/api/me/progress/ebook-1",
		map[string]string{"libraryItemId": testEbookID}, []byte(`{"isFinished":true}`), "1", testProfileID, h.handleSetItemProgress)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if store.upserted == nil || store.upserted.Progress != 1 {
		t.Fatalf("upserted progress = %#v, want 1", store.upserted)
	}
}

func TestSetEbookProgressRoutineAutosavePreservesCompletion(t *testing.T) {
	store := &recordingEbookProgressStore{row: &EbookProgress{
		UserID: "1", ProfileID: testProfileID, ContentID: testEbookID, FileID: 7, Progress: 1,
	}}
	media := &stubMediaStore{known: map[string]*models.MediaItem{
		testEbookID: {ContentID: testEbookID, Type: mediaTypeEbook},
	}}
	h := New(Dependencies{MediaStore: media, EbookProgressStore: store})
	rec := dispatchABSWithParams(http.MethodPost, "/api/me/progress/ebook-1",
		map[string]string{"libraryItemId": testEbookID}, []byte(`{"ebookProgress":0.2}`), "1", testProfileID, h.handleSetItemProgress)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if store.upserted == nil || store.upserted.Progress != 1 {
		t.Fatalf("upserted progress = %#v, want completed progress preserved", store.upserted)
	}
}

func TestSetEbookProgressReturnsAtomicallyCommittedCompletion(t *testing.T) {
	store := &recordingEbookProgressStore{
		row: &EbookProgress{
			UserID: "1", ProfileID: testProfileID, ContentID: testEbookID, FileID: 7, Progress: 0.4,
		},
		committed: &EbookProgress{
			UserID: "1", ProfileID: testProfileID, ContentID: testEbookID, FileID: 7, Progress: 1,
		},
	}
	media := &stubMediaStore{known: map[string]*models.MediaItem{
		testEbookID: {ContentID: testEbookID, Type: mediaTypeEbook},
	}}
	h := New(Dependencies{MediaStore: media, EbookProgressStore: store})
	rec := dispatchABSWithParams(http.MethodPost, "/api/me/progress/ebook-1",
		map[string]string{"libraryItemId": testEbookID}, []byte(`{"ebookProgress":0.2}`), "1", testProfileID, h.handleSetItemProgress)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ebookProgress":1`) || !strings.Contains(rec.Body.String(), `"isFinished":true`) {
		t.Fatalf("response did not reflect committed completion: %s", rec.Body.String())
	}
}

func TestContinueToggleUsesEbookHistoryWatermark(t *testing.T) {
	store := &recordingEbookProgressStore{row: &EbookProgress{UpdatedAt: time.Now()}}
	media := &stubMediaStore{known: map[string]*models.MediaItem{
		testEbookID: {ContentID: testEbookID, Type: mediaTypeEbook},
	}}
	h := New(Dependencies{MediaStore: media, EbookProgressStore: store})
	rec := dispatchABSWithParams(http.MethodGet, "/api/me/progress/ebook-1/remove-from-continue-listening",
		map[string]string{itemIDParam: testEbookID}, nil, "1", testProfileID, h.handleRemoveFromContinueListening)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if store.hidden == nil || !*store.hidden || store.hiddenItem != testEbookID {
		t.Fatalf("hidden call = (%v, %q), want (true, ebook-1)", store.hidden, store.hiddenItem)
	}
}
