package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const catalogSearchIndexBatchSize = 500

type SearchIndexProgressReporter interface {
	Report(percent float64, message string)
	SetResultData(data json.RawMessage)
}

type CatalogSearchIndexSyncStats struct {
	Configured      bool   `json:"configured"`
	Skipped         bool   `json:"skipped"`
	Reason          string `json:"reason,omitempty"`
	Events          int    `json:"events"`
	Upserted        int    `json:"upserted"`
	Deleted         int    `json:"deleted"`
	ActiveIndexUID  string `json:"active_index_uid,omitempty"`
	DocumentCount   int    `json:"document_count"`
	LastProcessedID int64  `json:"last_processed_event_id,omitempty"`
}

type CatalogSearchIndexRebuildStats struct {
	Configured     bool   `json:"configured"`
	Skipped        bool   `json:"skipped"`
	Reason         string `json:"reason,omitempty"`
	ActiveIndexUID string `json:"active_index_uid,omitempty"`
	DocumentCount  int    `json:"document_count"`
}

type CatalogSearchIndexer struct {
	pool          *pgxpool.Pool
	settingsStore SettingsStore
	events        *SearchIndexEventRepository
}

func NewCatalogSearchIndexer(pool *pgxpool.Pool, settingsStore SettingsStore) *CatalogSearchIndexer {
	return &CatalogSearchIndexer{
		pool:          pool,
		settingsStore: settingsStore,
		events:        NewSearchIndexEventRepository(pool),
	}
}

func (i *CatalogSearchIndexer) ShouldSyncRun(ctx context.Context) (bool, error) {
	settings, ok, err := i.loadMeilisearchRuntime(ctx)
	if err != nil || !ok || settings.Provider != SearchProviderMeilisearch {
		return false, err
	}
	state, err := i.events.GetState(ctx, SearchProviderMeilisearch)
	if err != nil {
		return false, err
	}
	if state.ActiveIndexUID == "" || state.SchemaVersion != SearchMeilisearchSchemaVersion {
		return false, nil
	}
	pending, err := i.events.PendingCount(ctx, SearchProviderMeilisearch)
	if err != nil {
		return false, err
	}
	return pending > 0, nil
}

func (i *CatalogSearchIndexer) SyncOutbox(ctx context.Context, progress SearchIndexProgressReporter) (CatalogSearchIndexSyncStats, error) {
	stats := CatalogSearchIndexSyncStats{}
	_, client, ok, err := i.loadClient(ctx)
	if err != nil {
		return stats, err
	}
	if !ok {
		stats.Skipped = true
		stats.Reason = "meilisearch is not configured"
		setSearchIndexTaskResult(progress, stats)
		reportSearchIndexProgress(progress, 100, "Meilisearch is not configured")
		return stats, nil
	}
	stats.Configured = true

	lock, locked, err := i.events.TryAdvisoryLock(ctx, searchIndexMaintenanceLockID)
	if err != nil {
		return stats, err
	}
	if !locked {
		stats.Skipped = true
		stats.Reason = "another search index maintenance task is running"
		setSearchIndexTaskResult(progress, stats)
		reportSearchIndexProgress(progress, 100, "Another catalog search index maintenance task is already running")
		return stats, nil
	}
	defer lock.Close(context.Background())

	state, err := i.events.GetState(ctx, SearchProviderMeilisearch)
	if err != nil {
		return stats, err
	}
	if state.ActiveIndexUID == "" || state.SchemaVersion != SearchMeilisearchSchemaVersion {
		stats.Skipped = true
		stats.Reason = "active search index is missing or stale; run rebuild_catalog_search_index"
		setSearchIndexTaskResult(progress, stats)
		reportSearchIndexProgress(progress, 100, "Catalog search index needs a rebuild")
		return stats, nil
	}
	stats.ActiveIndexUID = state.ActiveIndexUID

	reportSearchIndexProgress(progress, 0, "Loading pending catalog search events")
	events, err := i.events.ListPending(ctx, SearchProviderMeilisearch, catalogSearchIndexBatchSize)
	if err != nil {
		return stats, err
	}
	if len(events) == 0 {
		stats.DocumentCount = state.DocumentCount
		setSearchIndexTaskResult(progress, stats)
		reportSearchIndexProgress(progress, 100, "Catalog search index is already current")
		return stats, nil
	}
	stats.Events = len(events)
	ids := make([]int64, 0, len(events))
	maxID := int64(0)
	for _, event := range events {
		ids = append(ids, event.ID)
		if event.ID > maxID {
			maxID = event.ID
		}
	}

	upsertIDs, deleteIDs := coalesceSearchIndexEvents(events)
	reportSearchIndexProgress(progress, 20, "Building changed catalog search documents")
	docs, err := i.LoadDocumentsByIDs(ctx, upsertIDs)
	if err != nil {
		_ = i.events.MarkFailed(ctx, ids, err)
		return stats, err
	}
	found := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		found[doc.ContentID] = struct{}{}
	}
	for _, id := range upsertIDs {
		if _, ok := found[id]; !ok {
			deleteIDs = append(deleteIDs, id)
		}
	}
	deleteIDs = compactNonEmptyStrings(deleteIDs)

	if len(deleteIDs) > 0 {
		reportSearchIndexProgress(progress, 45, "Deleting stale catalog search documents")
		taskID, err := client.DeleteDocuments(ctx, state.ActiveIndexUID, deleteIDs)
		if err != nil {
			_ = i.events.MarkFailed(ctx, ids, err)
			return stats, err
		}
		if err := client.WaitTask(ctx, taskID); err != nil {
			_ = i.events.MarkFailed(ctx, ids, err)
			return stats, err
		}
		stats.Deleted = len(deleteIDs)
	}
	if len(docs) > 0 {
		reportSearchIndexProgress(progress, 70, "Upserting catalog search documents")
		taskID, err := client.AddDocuments(ctx, state.ActiveIndexUID, docs)
		if err != nil {
			_ = i.events.MarkFailed(ctx, ids, err)
			return stats, err
		}
		if err := client.WaitTask(ctx, taskID); err != nil {
			_ = i.events.MarkFailed(ctx, ids, err)
			return stats, err
		}
		stats.Upserted = len(docs)
	}

	docCount, err := client.Stats(ctx, state.ActiveIndexUID)
	if err != nil {
		_ = i.events.MarkFailed(ctx, ids, err)
		return stats, err
	}
	stats.DocumentCount = docCount
	stats.LastProcessedID = maxID
	if err := i.events.MarkProcessed(ctx, ids); err != nil {
		return stats, err
	}
	if err := i.events.UpdateStateAfterSync(ctx, SearchProviderMeilisearch, maxID, docCount); err != nil {
		return stats, err
	}
	setSearchIndexTaskResult(progress, stats)
	reportSearchIndexProgress(progress, 100, fmt.Sprintf("Synced %d catalog search events", len(events)))
	return stats, nil
}

func (i *CatalogSearchIndexer) Rebuild(ctx context.Context, progress SearchIndexProgressReporter) (CatalogSearchIndexRebuildStats, error) {
	stats := CatalogSearchIndexRebuildStats{}
	settings, client, ok, err := i.loadClient(ctx)
	if err != nil {
		return stats, err
	}
	if !ok {
		stats.Skipped = true
		stats.Reason = "meilisearch is not configured"
		setSearchIndexTaskResult(progress, stats)
		reportSearchIndexProgress(progress, 100, "Meilisearch is not configured")
		return stats, nil
	}
	stats.Configured = true

	lock, locked, err := i.events.TryAdvisoryLock(ctx, searchIndexMaintenanceLockID)
	if err != nil {
		return stats, err
	}
	if !locked {
		stats.Skipped = true
		stats.Reason = "another search index maintenance task is running"
		setSearchIndexTaskResult(progress, stats)
		reportSearchIndexProgress(progress, 100, "Another catalog search index maintenance task is already running")
		return stats, nil
	}
	defer lock.Close(context.Background())

	buildIndexUID := fmt.Sprintf("%s_rebuild_%d", settings.MeilisearchIndex, time.Now().Unix())
	stats.ActiveIndexUID = buildIndexUID
	reportSearchIndexProgress(progress, 0, "Creating catalog search index")
	taskID, err := client.CreateIndex(ctx, buildIndexUID)
	if err != nil {
		return stats, err
	}
	if err := client.WaitTask(ctx, taskID); err != nil {
		return stats, err
	}
	taskID, err = client.UpdateSettings(ctx, buildIndexUID, catalogSearchMeilisearchSettings())
	if err != nil {
		return stats, err
	}
	if err := client.WaitTask(ctx, taskID); err != nil {
		return stats, err
	}

	lastID := ""
	for {
		docs, err := i.LoadDocumentsAfter(ctx, lastID, catalogSearchIndexBatchSize)
		if err != nil {
			return stats, err
		}
		if len(docs) == 0 {
			break
		}
		taskID, err := client.AddDocuments(ctx, buildIndexUID, docs)
		if err != nil {
			return stats, err
		}
		if err := client.WaitTask(ctx, taskID); err != nil {
			return stats, err
		}
		stats.DocumentCount += len(docs)
		lastID = docs[len(docs)-1].ContentID
		reportSearchIndexProgress(progress, 25, fmt.Sprintf("Indexed %d catalog items", stats.DocumentCount))
	}
	docCount, err := client.Stats(ctx, buildIndexUID)
	if err != nil {
		return stats, err
	}
	stats.DocumentCount = docCount
	if err := i.events.UpdateStateAfterRebuild(ctx, SearchProviderMeilisearch, buildIndexUID, SearchMeilisearchSchemaVersion, docCount); err != nil {
		return stats, err
	}
	setSearchIndexTaskResult(progress, stats)
	reportSearchIndexProgress(progress, 100, fmt.Sprintf("Rebuilt catalog search index with %d documents", docCount))
	return stats, nil
}

func (i *CatalogSearchIndexer) loadClient(ctx context.Context) (CatalogSearchSettings, *meilisearchClient, bool, error) {
	settings, ok, err := i.loadMeilisearchRuntime(ctx)
	if err != nil || !ok {
		return settings, nil, ok, err
	}
	client, err := newMeilisearchClient(settings.MeilisearchURL, settings.MeilisearchAPIKey, settings.Timeout)
	if err != nil {
		return settings, nil, false, err
	}
	return settings, client, true, nil
}

func (i *CatalogSearchIndexer) loadMeilisearchRuntime(ctx context.Context) (CatalogSearchSettings, bool, error) {
	settings, err := LoadCatalogSearchSettings(ctx, i.settingsStore)
	if err != nil {
		return settings, false, err
	}
	if settings.Provider != SearchProviderMeilisearch || strings.TrimSpace(settings.MeilisearchURL) == "" {
		return settings, false, nil
	}
	return settings, true, nil
}

func (i *CatalogSearchIndexer) CheckConnection(ctx context.Context, settings CatalogSearchSettings) error {
	if settings.Provider != SearchProviderMeilisearch {
		return nil
	}
	client, err := newMeilisearchClient(settings.MeilisearchURL, settings.MeilisearchAPIKey, settings.Timeout)
	if err != nil {
		return err
	}
	return client.Health(ctx)
}

func catalogSearchMeilisearchSettings() map[string]any {
	return map[string]any{
		"displayedAttributes":  []string{"content_id", "type"},
		"filterableAttributes": []string{"type"},
		"searchableAttributes": []string{
			"title",
			"original_title",
			"sort_title",
			"title_variants",
			"people",
			"studios",
			"networks",
			"genres",
			"keywords",
			"overview",
			"tagline",
		},
		"pagination": map[string]any{
			"maxTotalHits": meilisearchDefaultCandidateScanCap,
		},
	}
}

func coalesceSearchIndexEvents(events []SearchIndexEvent) (upsertIDs []string, deleteIDs []string) {
	ops := make(map[string]string, len(events))
	for _, event := range events {
		switch event.Action {
		case SearchIndexEventRename:
			if event.PreviousContentID != "" {
				ops[event.PreviousContentID] = SearchIndexEventDelete
			}
			if event.ContentID != "" {
				ops[event.ContentID] = SearchIndexEventUpsert
			}
		case SearchIndexEventDelete:
			ops[event.ContentID] = SearchIndexEventDelete
		case SearchIndexEventUpsert:
			ops[event.ContentID] = SearchIndexEventUpsert
		}
	}
	for id, op := range ops {
		switch op {
		case SearchIndexEventDelete:
			deleteIDs = append(deleteIDs, id)
		case SearchIndexEventUpsert:
			upsertIDs = append(upsertIDs, id)
		}
	}
	return compactNonEmptyStrings(upsertIDs), compactNonEmptyStrings(deleteIDs)
}

func reportSearchIndexProgress(progress SearchIndexProgressReporter, percent float64, message string) {
	if progress != nil {
		progress.Report(percent, message)
	}
}

func setSearchIndexTaskResult(progress SearchIndexProgressReporter, result any) {
	if progress == nil {
		return
	}
	data, err := json.Marshal(result)
	if err == nil {
		progress.SetResultData(data)
	}
}

type catalogSearchDocument struct {
	ContentID     string   `json:"content_id"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	SortTitle     string   `json:"sort_title,omitempty"`
	OriginalTitle string   `json:"original_title,omitempty"`
	TitleVariants []string `json:"title_variants,omitempty"`
	Year          int      `json:"year,omitempty"`
	Overview      string   `json:"overview,omitempty"`
	Tagline       string   `json:"tagline,omitempty"`
	Genres        []string `json:"genres,omitempty"`
	Studios       []string `json:"studios,omitempty"`
	Networks      []string `json:"networks,omitempty"`
	Countries     []string `json:"countries,omitempty"`
	Keywords      []string `json:"keywords,omitempty"`
	People        []string `json:"people,omitempty"`
	SchemaVersion int      `json:"schema_version"`
}

func (i *CatalogSearchIndexer) LoadDocumentsAfter(ctx context.Context, afterContentID string, limit int) ([]catalogSearchDocument, error) {
	if i == nil || i.pool == nil || limit <= 0 {
		return nil, nil
	}
	rows, err := i.pool.Query(ctx, catalogSearchDocumentSelectSQL(`
		WHERE ($1::text = '' OR mi.content_id > $1)
		  AND NOT EXISTS (
			SELECT 1 FROM manga_chapters mc
			WHERE mc.chapter_content_id = mi.content_id
		  )
	`, `
		ORDER BY mi.content_id ASC
		LIMIT $2
	`), afterContentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalogSearchDocuments(rows)
}

func (i *CatalogSearchIndexer) LoadDocumentsByIDs(ctx context.Context, contentIDs []string) ([]catalogSearchDocument, error) {
	contentIDs = compactNonEmptyStrings(contentIDs)
	if i == nil || i.pool == nil || len(contentIDs) == 0 {
		return nil, nil
	}
	rows, err := i.pool.Query(ctx, catalogSearchDocumentSelectSQL(`
		WHERE mi.content_id = ANY($1)
		  AND NOT EXISTS (
			SELECT 1 FROM manga_chapters mc
			WHERE mc.chapter_content_id = mi.content_id
		  )
	`, `
		ORDER BY mi.content_id ASC
	`), contentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalogSearchDocuments(rows)
}

func catalogSearchDocumentSelectSQL(whereClause string, tailClause string) string {
	return `
		SELECT
			mi.content_id,
			mi.type,
			COALESCE(mi.title, ''),
			COALESCE(mi.sort_title, ''),
			COALESCE(mi.original_title, ''),
			COALESCE(mi.year, 0),
			COALESCE(mi.overview, ''),
			COALESCE(mi.tagline, ''),
			COALESCE(mi.genres, ARRAY[]::text[]),
			COALESCE(mi.studios, ARRAY[]::text[]),
			COALESCE(mi.networks, ARRAY[]::text[]),
			COALESCE(mi.countries, ARRAY[]::text[]),
			COALESCE(mi.keywords, ARRAY[]::text[]),
			COALESCE(array_agg(DISTINCT p.name) FILTER (WHERE p.name IS NOT NULL AND p.name <> ''), ARRAY[]::text[]) AS people
		FROM media_items mi
		LEFT JOIN item_people ip ON ip.content_id = mi.content_id
		LEFT JOIN people p ON p.id = ip.person_id
	` + whereClause + `
		GROUP BY
			mi.content_id, mi.type, mi.title, mi.sort_title, mi.original_title, mi.year,
			mi.overview, mi.tagline, mi.genres, mi.studios, mi.networks, mi.countries, mi.keywords
	` + tailClause
}

func scanCatalogSearchDocuments(rows pgx.Rows) ([]catalogSearchDocument, error) {
	var docs []catalogSearchDocument
	for rows.Next() {
		var doc catalogSearchDocument
		if err := rows.Scan(
			&doc.ContentID,
			&doc.Type,
			&doc.Title,
			&doc.SortTitle,
			&doc.OriginalTitle,
			&doc.Year,
			&doc.Overview,
			&doc.Tagline,
			&doc.Genres,
			&doc.Studios,
			&doc.Networks,
			&doc.Countries,
			&doc.Keywords,
			&doc.People,
		); err != nil {
			return nil, err
		}
		doc.SchemaVersion = SearchMeilisearchSchemaVersion
		doc.TitleVariants = catalogSearchTitleVariants(doc)
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func catalogSearchTitleVariants(doc catalogSearchDocument) []string {
	return compactNonEmptyStrings([]string{
		doc.Title,
		doc.SortTitle,
		doc.OriginalTitle,
		normalizeTitleForComparison(doc.Title),
		normalizeTitleForComparison(doc.SortTitle),
		normalizeTitleForComparison(doc.OriginalTitle),
	})
}
