package catalog

import (
	"context"
	"log/slog"
)

type CatalogSearchService struct {
	settings CatalogSearchSettings
	provider CatalogSearchProvider
	meili    *MeilisearchSearchProvider
	state    *SearchIndexEventRepository
	itemRepo *ItemRepository
}

func NewCatalogSearchService(
	ctx context.Context,
	settingsStore SettingsStore,
	itemRepo *ItemRepository,
	stateRepo *SearchIndexEventRepository,
	vectorizer ...CatalogSearchQueryVectorizer,
) *CatalogSearchService {
	settings, err := LoadCatalogSearchSettings(ctx, settingsStore)
	if err != nil {
		slog.Warn("catalog search: failed to load settings; using postgres", "err", err)
		settings = DefaultCatalogSearchSettings()
	}
	return NewCatalogSearchServiceFromSettings(settings, itemRepo, stateRepo, vectorizer...)
}

func NewCatalogSearchServiceFromSettings(
	settings CatalogSearchSettings,
	itemRepo *ItemRepository,
	stateRepo *SearchIndexEventRepository,
	vectorizer ...CatalogSearchQueryVectorizer,
) *CatalogSearchService {
	fallback := NewPostgresSearchProvider(itemRepo)
	service := &CatalogSearchService{
		settings: settings,
		provider: fallback,
		state:    stateRepo,
		itemRepo: itemRepo,
	}
	if settings.Provider != SearchProviderMeilisearch {
		return service
	}
	if settings.MeilisearchURL == "" {
		slog.Warn("catalog search: meilisearch selected without URL; using postgres")
		return service
	}
	var queryVectorizer CatalogSearchQueryVectorizer
	if len(vectorizer) > 0 {
		queryVectorizer = vectorizer[0]
	}
	meili, err := NewMeilisearchSearchProvider(itemRepo, stateRepo, fallback, MeilisearchProviderConfig{
		URL:              settings.MeilisearchURL,
		APIKey:           settings.MeilisearchAPIKey,
		Index:            settings.MeilisearchIndex,
		Timeout:          settings.Timeout,
		MatchingStrategy: settings.MatchingStrategy,
		IndexTypes:       settings.IndexTypes,
		SemanticEnabled:  settings.SemanticEnabled,
		SemanticRatio:    settings.SemanticRatio,
		Embedder:         settings.Embedder,
		Vectorizer:       queryVectorizer,
	})
	if err != nil {
		slog.Warn("catalog search: failed to initialize meilisearch provider; using postgres", "err", err)
		return service
	}
	service.provider = meili
	service.meili = meili
	return service
}

func (s *CatalogSearchService) Provider() CatalogSearchProvider {
	if s == nil || s.provider == nil {
		return nil
	}
	return s.provider
}

func (s *CatalogSearchService) Status(ctx context.Context) CatalogSearchRuntimeStatus {
	settings := DefaultCatalogSearchSettings()
	if s != nil {
		settings = s.settings
	}
	status := CatalogSearchRuntimeStatus{
		ConfiguredProvider: settings.Provider,
		ActiveProvider:     SearchProviderPostgres,
		Meilisearch: CatalogSearchMeiliStatus{
			Configured:       settings.Provider == SearchProviderMeilisearch && settings.MeilisearchURL != "",
			Healthy:          false,
			CircuitState:     "not_configured",
			TimeoutMS:        int(settings.Timeout.Milliseconds()),
			MatchingStrategy: settings.MatchingStrategy,
			IndexTypes:       settings.IndexTypes,
			SemanticEnabled:  settings.SemanticEnabled,
			SemanticRatio:    settings.SemanticRatio,
			Embedder:         settings.Embedder,
		},
		Index: CatalogSearchIndexStateStatus{
			ExpectedSchemaVersion: catalogSearchMeilisearchSchemaVersion(settings.Embedder, settings.IndexTypes),
		},
		Tasks: []CatalogSearchTaskLink{
			{Key: "sync_catalog_search_index", Name: "Sync Catalog Search Index", Href: "/admin/tasks/sync_catalog_search_index"},
			{Key: "rebuild_catalog_search_index", Name: "Rebuild Catalog Search Index", Href: "/admin/tasks/rebuild_catalog_search_index"},
		},
	}
	if s != nil {
		if _, ok := s.provider.(*MeilisearchSearchProvider); ok {
			status.ActiveProvider = SearchProviderMeilisearch
		}
		if s.meili != nil {
			status.Meilisearch = s.meili.Status()
		}
		if s.state != nil {
			state, err := s.state.GetState(ctx, SearchProviderMeilisearch)
			if err == nil {
				status.Index.ActiveIndexUID = state.ActiveIndexUID
				status.Index.SchemaVersion = state.SchemaVersion
				status.Index.DocumentCount = state.DocumentCount
				status.Index.LastRebuildAt = state.LastRebuildAt
				status.Index.LastSyncAt = state.LastSyncAt
				status.Index.LastProcessedEventID = state.LastProcessedEventID
			}
			if pending, err := s.state.PendingCount(ctx, SearchProviderMeilisearch); err == nil {
				status.Index.PendingEvents = pending
			}
		}
		if s.itemRepo != nil && s.itemRepo.pool != nil {
			if vectorCount, err := countCatalogSearchVectorDocuments(ctx, s.itemRepo.pool, settings.IndexTypes); err == nil {
				status.Index.VectorDocumentCount = vectorCount
			}
		}
	}
	return status
}
