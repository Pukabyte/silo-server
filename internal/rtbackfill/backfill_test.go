package rtbackfill

import (
	"context"
	"errors"
	"testing"

	"github.com/Silo-Server/silo-server/internal/catalog"
	"github.com/Silo-Server/silo-server/internal/mdblist"
)

type stubRTFetcher struct {
	ratings *mdblist.TitleRatings
	err     error
	calls   int
}

func (s *stubRTFetcher) RatingsByIMDB(_ context.Context, _ string) (*mdblist.TitleRatings, error) {
	s.calls++
	return s.ratings, s.err
}

type stubRTUpdater struct {
	last  *catalog.MetadataUpdate
	calls int
	err   error
}

func (s *stubRTUpdater) UpdateMetadata(_ context.Context, _ string, upd *catalog.MetadataUpdate) error {
	s.calls++
	s.last = upd
	return s.err
}

func f64(v float64) *float64 { return &v }

func TestRTBackfillPersistsScores(t *testing.T) {
	fetcher := &stubRTFetcher{ratings: &mdblist.TitleRatings{RTCritic: f64(89), RTAudience: f64(94)}}
	updater := &stubRTUpdater{}
	b := New(fetcher, updater)
	if b == nil {
		t.Fatal("New returned nil for valid deps")
	}

	b.run("ct1", "tt0111161") // synchronous run avoids goroutine races in the test

	if updater.calls != 1 {
		t.Fatalf("expected 1 update, got %d", updater.calls)
	}
	if updater.last.RatingRTCritic == nil || *updater.last.RatingRTCritic != 89 {
		t.Fatalf("critic not persisted: %+v", updater.last.RatingRTCritic)
	}
	if updater.last.RatingRTAudience == nil || *updater.last.RatingRTAudience != 94 {
		t.Fatalf("audience not persisted: %+v", updater.last.RatingRTAudience)
	}
}

func TestRTBackfillNegativeCacheOnNoRT(t *testing.T) {
	fetcher := &stubRTFetcher{ratings: &mdblist.TitleRatings{}} // no RT
	b := New(fetcher, &stubRTUpdater{})

	b.run("ct2", "tt2") // no RTCritic -> negative-cached, no persist

	// A subsequent claim must be refused while the negative cache is warm.
	if b.claim("ct2") {
		t.Fatal("expected negative cache to refuse a repeat claim")
	}
}

func TestRTBackfillDedupeInFlight(t *testing.T) {
	b := New(&stubRTFetcher{}, &stubRTUpdater{})
	if !b.claim("ct3") {
		t.Fatal("first claim should succeed")
	}
	if b.claim("ct3") {
		t.Fatal("second claim should be refused while in flight")
	}
}

func TestRTBackfillNilSafety(t *testing.T) {
	if New(nil, &stubRTUpdater{}) != nil {
		t.Fatal("nil fetcher should yield nil backfiller")
	}
	var b *Backfiller
	b.Enqueue("x", "tt1") // must not panic
}

func TestRTBackfillFetchErrorNoPersist(t *testing.T) {
	fetcher := &stubRTFetcher{err: errors.New("boom")}
	updater := &stubRTUpdater{}
	b := New(fetcher, updater)

	b.run("ct4", "tt4")

	if updater.calls != 0 {
		t.Fatalf("no update expected on fetch error, got %d", updater.calls)
	}
}

// A transient fetch error must NOT negative-cache the item, so the next view
// can retry it immediately.
func TestRTBackfillFetchErrorDoesNotNegativeCache(t *testing.T) {
	b := New(&stubRTFetcher{err: errors.New("429")}, &stubRTUpdater{})
	b.run("ct5", "tt5")
	if !b.claim("ct5") {
		t.Fatal("fetch error must not negative-cache; a repeat claim should succeed")
	}
}

// A persist failure is also transient and must not negative-cache.
func TestRTBackfillPersistErrorDoesNotNegativeCache(t *testing.T) {
	fetcher := &stubRTFetcher{ratings: &mdblist.TitleRatings{RTCritic: f64(90)}}
	updater := &stubRTUpdater{err: errors.New("db down")}
	b := New(fetcher, updater)
	b.run("ct6", "tt6")
	if !b.claim("ct6") {
		t.Fatal("persist error must not negative-cache; a repeat claim should succeed")
	}
}
