package handlers

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Silo-Server/silo-server/internal/catalog"
	"github.com/Silo-Server/silo-server/internal/mdblist"
)

// rtRatingsFetcher fetches external ratings by IMDb id. Satisfied by
// *mdblist.Client; kept as an interface so the handler package does not depend
// on the concrete client and can be tested with a stub.
type rtRatingsFetcher interface {
	RatingsByIMDB(ctx context.Context, imdbID string) (*mdblist.TitleRatings, error)
}

// rtItemUpdater persists a partial metadata update. Satisfied by
// *catalog.ItemRepository.
type rtItemUpdater interface {
	UpdateMetadata(ctx context.Context, contentID string, upd *catalog.MetadataUpdate) error
}

const (
	// rtBackfillTimeout bounds a single MDBList lookup so a slow upstream can
	// never leak goroutines.
	rtBackfillTimeout = 10 * time.Second
	// rtBackfillNegativeTTL is how long a content id that returned no RT score
	// is skipped, so opening a title MDBList has no RT for does not hammer it on
	// every view.
	rtBackfillNegativeTTL = 24 * time.Hour
)

// rtBackfiller lazily populates Rotten Tomatoes scores on view. It is
// best-effort and fully asynchronous: Enqueue returns immediately and the
// fetch/persist runs in a detached goroutine. In-flight dedupe stops concurrent
// duplicate lookups for the same item, and a negative cache stops re-fetching
// titles MDBList has no RT score for.
type rtBackfiller struct {
	fetcher rtRatingsFetcher
	updater rtItemUpdater

	mu       sync.Mutex
	inflight map[string]struct{}
	negative map[string]time.Time
	now      func() time.Time // injectable for tests
}

func newRTBackfiller(fetcher rtRatingsFetcher, updater rtItemUpdater) *rtBackfiller {
	if fetcher == nil || updater == nil {
		return nil
	}
	return &rtBackfiller{
		fetcher:  fetcher,
		updater:  updater,
		inflight: make(map[string]struct{}),
		negative: make(map[string]time.Time),
		now:      time.Now,
	}
}

// Enqueue schedules an async RT backfill for contentID (a movie or series)
// keyed by imdbID. It is a no-op when a lookup is already in flight or the id
// was recently found to have no RT score.
func (b *rtBackfiller) Enqueue(contentID, imdbID string) {
	if b == nil || contentID == "" || imdbID == "" {
		return
	}
	if !b.claim(contentID) {
		return
	}
	go b.run(contentID, imdbID)
}

// claim reserves contentID for a lookup, returning false when it is already
// in flight or negatively cached.
func (b *rtBackfiller) claim(contentID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.inflight[contentID]; ok {
		return false
	}
	if until, ok := b.negative[contentID]; ok {
		if b.now().Before(until) {
			return false
		}
		delete(b.negative, contentID)
	}
	b.inflight[contentID] = struct{}{}
	return true
}

func (b *rtBackfiller) release(contentID string, foundRT bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inflight, contentID)
	if !foundRT {
		b.negative[contentID] = b.now().Add(rtBackfillNegativeTTL)
	}
}

func (b *rtBackfiller) run(contentID, imdbID string) {
	ctx, cancel := context.WithTimeout(context.Background(), rtBackfillTimeout)
	defer cancel()

	foundRT := false
	defer func() { b.release(contentID, foundRT) }()

	ratings, err := b.fetcher.RatingsByIMDB(ctx, imdbID)
	if err != nil {
		slog.Warn("catalog: mdblist rotten tomatoes backfill failed",
			"content_id", contentID, "imdb", imdbID, "error", err)
		return
	}
	if ratings == nil || ratings.RTCritic == nil {
		// Nothing to store; negative-cache so we don't retry on every open.
		return
	}

	upd := &catalog.MetadataUpdate{}
	critic := int(*ratings.RTCritic)
	upd.RatingRTCritic = &critic
	if ratings.RTAudience != nil {
		audience := int(*ratings.RTAudience)
		upd.RatingRTAudience = &audience
	}
	if err := b.updater.UpdateMetadata(ctx, contentID, upd); err != nil {
		slog.Warn("catalog: persisting rotten tomatoes backfill failed",
			"content_id", contentID, "imdb", imdbID, "error", err)
		return
	}
	foundRT = true
}
