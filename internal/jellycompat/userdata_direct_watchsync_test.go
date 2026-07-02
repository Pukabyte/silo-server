package jellycompat

import (
	"context"
	"testing"
	"time"

	"github.com/Silo-Server/silo-server/internal/userstore"
	"github.com/Silo-Server/silo-server/internal/watchstate"
	"github.com/Silo-Server/silo-server/internal/watchsync"
)

// fakeLocalWatchDispatcher captures the local watch events emitted by the
// jellycompat mark-played/unplayed surface so tests can assert that provider
// export is triggered (previously it was silently dropped).
type fakeLocalWatchDispatcher struct {
	events []watchsync.LocalWatchEvent
}

func (f *fakeLocalWatchDispatcher) HandleLocalWatchEvent(_ context.Context, event watchsync.LocalWatchEvent) error {
	f.events = append(f.events, event)
	return nil
}

// fakeWatchStore embeds userstore.UserStore so only the methods exercised by the
// mark-played/unplayed paths need real implementations; any other call panics.
type fakeWatchStore struct {
	userstore.UserStore
	completed []userstore.WatchHistoryEntry
	removed   []string
}

func (f *fakeWatchStore) MarkWatched(_ context.Context, _, _ string, _ float64) error {
	return nil
}

func (f *fakeWatchStore) AddVisibleHistory(_ context.Context, entry userstore.WatchHistoryEntry) (userstore.WatchHistoryEntry, error) {
	if entry.ID == "" {
		entry.ID = "hist-" + entry.MediaItemID
	}
	// Populate a provider-identifiable stable identity so the entry yields a
	// LocalPlay (LocalPlaysFromHistory drops entries without provider IDs).
	entry.Identity = userstore.WatchIdentity{
		StableType:  "movie",
		ProviderIDs: map[string]string{"imdb": "tt-" + entry.MediaItemID},
	}
	return entry, nil
}

func (f *fakeWatchStore) ListCompletedHistory(_ context.Context, _ userstore.CompletedHistoryQuery) ([]userstore.WatchHistoryEntry, error) {
	return f.completed, nil
}

func (f *fakeWatchStore) RemoveHistoryItems(_ context.Context, _ string, mediaItemIDs []string, _ time.Time) error {
	f.removed = append(f.removed, mediaItemIDs...)
	return nil
}

type fakeStoreProvider struct {
	store userstore.UserStore
}

func (f fakeStoreProvider) ForUser(_ context.Context, _ int) (userstore.UserStore, error) {
	return f.store, nil
}

func (f fakeStoreProvider) Close() error { return nil }

func newTestDirectUserDataService(store userstore.UserStore, dispatcher watchsync.LocalWatchEventDispatcher) *directUserDataService {
	return &directUserDataService{
		watchState:           watchstate.NewService(fakeStoreProvider{store: store}),
		localWatchDispatcher: dispatcher,
	}
}

func TestMarkPlayedDispatchesLocalWatchEvent(t *testing.T) {
	store := &fakeWatchStore{}
	dispatcher := &fakeLocalWatchDispatcher{}
	svc := newTestDirectUserDataService(store, dispatcher)
	session := &Session{StreamAppUserID: 1, ProfileID: "profile-1"}

	if err := svc.MarkPlayed(context.Background(), session, "movie-1"); err != nil {
		t.Fatalf("MarkPlayed returned error: %v", err)
	}

	if len(dispatcher.events) != 1 {
		t.Fatalf("expected one local watch event; got %d", len(dispatcher.events))
	}
	event := dispatcher.events[0]
	if event.Kind != watchsync.LocalWatchEventMarkedWatched {
		t.Errorf("expected kind %q; got %q", watchsync.LocalWatchEventMarkedWatched, event.Kind)
	}
	if event.UserID != 1 || event.ProfileID != "profile-1" {
		t.Errorf("unexpected event scope: userID=%d profileID=%q", event.UserID, event.ProfileID)
	}
	if len(event.Plays) != 1 {
		t.Fatalf("expected one play; got %d", len(event.Plays))
	}
	if event.Plays[0].MediaItemID != "movie-1" {
		t.Errorf("expected play for movie-1; got %q", event.Plays[0].MediaItemID)
	}
}

func TestMarkUnplayedDispatchesLocalWatchEvent(t *testing.T) {
	store := &fakeWatchStore{
		completed: []userstore.WatchHistoryEntry{{
			ID:          "hist-1",
			MediaItemID: "movie-1",
			WatchedAt:   time.Now().UTC().Format(time.RFC3339),
			Completed:   true,
			Source:      userstore.WatchHistorySourceJellycompat,
			Identity: userstore.WatchIdentity{
				StableType:  "movie",
				ProviderIDs: map[string]string{"imdb": "tt-movie-1"},
			},
		}},
	}
	dispatcher := &fakeLocalWatchDispatcher{}
	svc := newTestDirectUserDataService(store, dispatcher)
	session := &Session{StreamAppUserID: 1, ProfileID: "profile-1"}

	if err := svc.MarkUnplayed(context.Background(), session, "movie-1"); err != nil {
		t.Fatalf("MarkUnplayed returned error: %v", err)
	}

	if len(store.removed) != 1 || store.removed[0] != "movie-1" {
		t.Errorf("expected movie-1 history removed; got %v", store.removed)
	}
	if len(dispatcher.events) != 1 {
		t.Fatalf("expected one local watch event; got %d", len(dispatcher.events))
	}
	event := dispatcher.events[0]
	if event.Kind != watchsync.LocalWatchEventMarkedUnwatched {
		t.Errorf("expected kind %q; got %q", watchsync.LocalWatchEventMarkedUnwatched, event.Kind)
	}
	if len(event.Plays) != 1 || event.Plays[0].MediaItemID != "movie-1" {
		t.Fatalf("expected one play for movie-1; got %+v", event.Plays)
	}
}

func TestMarkPlayedWithoutDispatcherIsNoOp(t *testing.T) {
	store := &fakeWatchStore{}
	svc := newTestDirectUserDataService(store, nil)
	session := &Session{StreamAppUserID: 1, ProfileID: "profile-1"}

	if err := svc.MarkPlayed(context.Background(), session, "movie-1"); err != nil {
		t.Fatalf("MarkPlayed returned error: %v", err)
	}
}
