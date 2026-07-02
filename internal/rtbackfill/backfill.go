// Package rtbackfill lazily populates Rotten Tomatoes scores on view. It is the
// single implementation shared by the native catalog API and the Jellyfin
// compatibility surface so both trigger the same best-effort, fully async fill.
package rtbackfill

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Silo-Server/silo-server/internal/catalog"
	"github.com/Silo-Server/silo-server/internal/mdblist"
)

// Fetcher fetches external ratings by IMDb id. Satisfied by *mdblist.Client;
// kept as an interface so callers do not depend on the concrete client and can
// be tested with a stub.
type Fetcher interface {
	RatingsByIMDB(ctx context.Context, imdbID string) (*mdblist.TitleRatings, error)
}

// Updater persists a partial metadata update. Satisfied by
// *catalog.ItemRepository.
type Updater interface {
	UpdateMetadata(ctx context.Context, contentID string, upd *catalog.MetadataUpdate) error
}

const (
	// backfillTimeout bounds a single MDBList lookup so a slow upstream can
	// never leak goroutines.
	backfillTimeout = 10 * time.Second
	// negativeTTL is how long a content id that returned no RT score is
	// skipped, so opening a title MDBList has no RT for does not hammer it on
	// every view.
	negativeTTL = 24 * time.Hour
)

// Backfiller lazily populates Rotten Tomatoes scores on view. It is best-effort
// and fully asynchronous: Enqueue returns immediately and the fetch/persist
// runs in a detached goroutine. In-flight dedupe stops concurrent duplicate
// lookups for the same item, and a negative cache stops re-fetching titles
// MDBList has no RT score for.
type Backfiller struct {
	fetcher Fetcher
	updater Updater

	mu       sync.Mutex
	inflight map[string]struct{}
	negative map[string]time.Time
	now      func() time.Time // injectable for tests
}

// New builds a Backfiller. Returns nil when either dependency is nil, so
// callers can wire it unconditionally and rely on Enqueue's nil-safety.
func New(fetcher Fetcher, updater Updater) *Backfiller {
	if fetcher == nil || updater == nil {
		return nil
	}
	return &Backfiller{
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
func (b *Backfiller) Enqueue(contentID, imdbID string) {
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
func (b *Backfiller) claim(contentID string) bool {
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

func (b *Backfiller) release(contentID string, foundRT bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inflight, contentID)
	if !foundRT {
		b.negative[contentID] = b.now().Add(negativeTTL)
	}
}

func (b *Backfiller) run(contentID, imdbID string) {
	ctx, cancel := context.WithTimeout(context.Background(), backfillTimeout)
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
