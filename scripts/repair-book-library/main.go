package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Silo-Server/silo-server/internal/bookmeta"
	"github.com/Silo-Server/silo-server/internal/titleutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type auditReport struct {
	Mode                  string               `json:"mode"`
	InvalidCredits        []invalidCredit      `json:"invalid_credits"`
	SeriesRepairs         []seriesRepair       `json:"series_repairs"`
	TitleRepairs          []titleRepair        `json:"title_repairs"`
	SuspiciousTitleCounts map[string]int       `json:"suspicious_title_counts"`
	MultiDiscParentGroups int                  `json:"multi_disc_parent_groups"`
	MultiDiscCurrentItems int                  `json:"multi_disc_current_items"`
	MultiDiscCandidates   []multiDiscCandidate `json:"multi_disc_candidates"`
	AppliedInvalidCredits int64                `json:"applied_invalid_credits"`
	AppliedSeriesRepairs  int64                `json:"applied_series_repairs"`
	AppliedTitleRepairs   int64                `json:"applied_title_repairs"`
}

type invalidCredit struct {
	ContentID string `json:"content_id"`
	MediaType string `json:"media_type"`
	PersonID  int64  `json:"person_id"`
	Kind      int    `json:"kind"`
	Name      string `json:"name"`
}

type seriesRepair struct {
	Table     string `json:"table"`
	ContentID string `json:"content_id"`
	OldName   string `json:"old_name"`
	NewName   string `json:"new_name"`
	OldKey    string `json:"old_key"`
	NewKey    string `json:"new_key"`
}

type titleRepair struct {
	ContentID string `json:"content_id"`
	OldTitle  string `json:"old_title"`
	NewTitle  string `json:"new_title"`
}

type multiDiscCandidate struct {
	ParentPath  string   `json:"parent_path"`
	CanonicalID string   `json:"canonical_id"`
	SourceIDs   []string `json:"source_ids"`
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func validAutomaticCredit(name string) bool {
	return bookmeta.TrustedAutomaticCredit(name)
}

func main() {
	mode := flag.String("mode", "audit", "audit, canary, or apply")
	limit := flag.Int("limit", 0, "maximum repairs per class; zero means unlimited")
	flag.Parse()
	if *mode != "audit" && *mode != "canary" && *mode != "apply" {
		fatalf("unsupported mode %q", *mode)
	}
	if *mode == "canary" && *limit <= 0 {
		fatalf("canary mode requires -limit")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		fatalf("DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fatalf("connect: %v", err)
	}
	defer pool.Close()

	report, err := run(ctx, pool, *mode, *limit)
	if err != nil {
		fatalf("repair: %v", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fatalf("encode report: %v", err)
	}
}

func run(ctx context.Context, pool *pgxpool.Pool, mode string, limit int) (auditReport, error) {
	report := auditReport{Mode: mode, SuspiciousTitleCounts: map[string]int{}}
	credits, err := loadInvalidCredits(ctx, pool, limit)
	if err != nil {
		return report, err
	}
	report.InvalidCredits = credits
	series, err := loadSeriesRepairs(ctx, pool, limit)
	if err != nil {
		return report, err
	}
	report.SeriesRepairs = series
	titles, err := loadTitleRepairs(ctx, pool, limit)
	if err != nil {
		return report, err
	}
	report.TitleRepairs = titles
	if err := loadSuspiciousTitleCounts(ctx, pool, report.SuspiciousTitleCounts); err != nil {
		return report, err
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT parent_path), COUNT(DISTINCT content_id)
		FROM (
			SELECT regexp_replace(mf.observed_root_path, '/(cd|disc|disk|part)[ _.-]*[0-9]+$', '', 'i') AS parent_path,
			       mi.content_id
			FROM media_files mf
			JOIN media_items mi ON mi.content_id = mf.content_id
			WHERE mi.type = 'audiobook'
			  AND mf.observed_root_path ~* '/(cd|disc|disk|part)[ _.-]*[0-9]+$'
		) candidates`).Scan(&report.MultiDiscParentGroups, &report.MultiDiscCurrentItems); err != nil {
		return report, fmt.Errorf("audit multi-disc candidates: %w", err)
	}
	multiDisc, err := loadMultiDiscCandidates(ctx, pool, limit)
	if err != nil {
		return report, err
	}
	report.MultiDiscCandidates = multiDisc
	if mode == "apply" || mode == "canary" {
		if err := applyRepairs(ctx, pool, limit, &report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func loadInvalidCredits(ctx context.Context, db queryer, limit int) ([]invalidCredit, error) {
	rows, err := db.Query(ctx, `
		SELECT mi.content_id, mi.type, p.id, ip.kind, p.name
		FROM item_people ip
		JOIN people p ON p.id = ip.person_id
		JOIN media_items mi ON mi.content_id = ip.content_id
		WHERE mi.type IN ('ebook', 'audiobook')
		  AND ip.kind IN (7, 8)
		  AND lower(btrim(COALESCE(mi.status, ''))) <> 'curated'
		ORDER BY mi.type, mi.content_id, ip.kind, p.id`)
	if err != nil {
		return nil, fmt.Errorf("query credits: %w", err)
	}
	defer rows.Close()
	out := make([]invalidCredit, 0)
	for rows.Next() {
		var c invalidCredit
		if err := rows.Scan(&c.ContentID, &c.MediaType, &c.PersonID, &c.Kind, &c.Name); err != nil {
			return nil, err
		}
		if validAutomaticCredit(c.Name) {
			continue
		}
		out = append(out, c)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

func loadSeriesRepairs(ctx context.Context, db queryer, limit int) ([]seriesRepair, error) {
	out := make([]seriesRepair, 0)
	for _, table := range []string{"audiobook_series", "ebook_series"} {
		rows, err := db.Query(ctx, `SELECT content_id, series_name, series_key FROM `+table+` ORDER BY content_id`)
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", table, err)
		}
		for rows.Next() {
			var contentID, oldName, oldKey string
			if err := rows.Scan(&contentID, &oldName, &oldKey); err != nil {
				rows.Close()
				return nil, err
			}
			newName := bookmeta.CleanSeriesDisplay(oldName)
			newKey := bookmeta.NormalizeSeriesKey(newName)
			if newName == oldName && newKey == oldKey {
				continue
			}
			out = append(out, seriesRepair{table, contentID, oldName, newName, oldKey, newKey})
			if limit > 0 && len(out) >= limit {
				break
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func loadMultiDiscCandidates(ctx context.Context, db queryer, limit int) ([]multiDiscCandidate, error) {
	rows, err := db.Query(ctx, `
		WITH parts AS (
			SELECT mi.content_id, mi.status, mi.created_at,
			       lower(regexp_replace(btrim(mi.title), '[^[:alnum:]]', '', 'g')) AS title_key,
			       regexp_replace(mf.observed_root_path, '/(cd|disc|disk|part)[ _.-]*[0-9]+$', '', 'i') AS parent_path,
			       (regexp_match(mf.observed_root_path, '(?i)/(cd|disc|disk|part)[ _.-]*([0-9]+)$'))[2]::int AS part_number
			FROM media_files mf
			JOIN media_items mi ON mi.content_id = mf.content_id
			WHERE mi.type = 'audiobook'
			  AND mf.observed_root_path ~* '/(cd|disc|disk|part)[ _.-]*[0-9]+$'
		), items AS (
			SELECT content_id, status, created_at, title_key, parent_path,
			       MIN(part_number) AS first_part, MAX(part_number) AS last_part
			FROM parts
			GROUP BY content_id, status, created_at, title_key, parent_path
		), candidates AS (
			SELECT parent_path,
			       array_agg(content_id ORDER BY CASE WHEN lower(btrim(COALESCE(status, ''))) = 'curated' THEN 0 ELSE 1 END, created_at, content_id) AS content_ids,
			       MIN(first_part) AS first_part,
			       MAX(last_part) AS last_part,
			       COUNT(DISTINCT title_key) AS title_count
			FROM items
			GROUP BY parent_path
			HAVING COUNT(*) > 1
		)
		SELECT parent_path, content_ids
		FROM candidates
		WHERE first_part = 1
		  AND last_part = cardinality(content_ids)
		  AND title_count = 1
		ORDER BY parent_path
	`)
	if err != nil {
		return nil, fmt.Errorf("query multi-disc repair candidates: %w", err)
	}
	defer rows.Close()
	var out []multiDiscCandidate
	for rows.Next() {
		var parentPath string
		var ids []string
		if err := rows.Scan(&parentPath, &ids); err != nil {
			return nil, err
		}
		if len(ids) < 2 {
			continue
		}
		out = append(out, multiDiscCandidate{ParentPath: parentPath, CanonicalID: ids[0], SourceIDs: append([]string(nil), ids[1:]...)})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

func loadTitleRepairs(ctx context.Context, db queryer, limit int) ([]titleRepair, error) {
	rows, err := db.Query(ctx, `
		SELECT mi.content_id, mi.title, MIN(mf.file_path),
		       COALESCE(array_agg(DISTINCT p.name) FILTER (WHERE ip.kind = 7), ARRAY[]::text[])
		FROM media_items mi
		JOIN media_files mf ON mf.content_id = mi.content_id
		LEFT JOIN item_people ip ON ip.content_id = mi.content_id
		LEFT JOIN people p ON p.id = ip.person_id
		WHERE mi.type = 'ebook'
		  AND lower(btrim(COALESCE(mi.status, ''))) <> 'curated'
		  AND lower(btrim(mi.title)) = ANY($1::text[])
		GROUP BY mi.content_id, mi.title
		ORDER BY mi.content_id
	`, suspiciousEbookTitles())
	if err != nil {
		return nil, fmt.Errorf("query suspicious ebook title repairs: %w", err)
	}
	defer rows.Close()
	var out []titleRepair
	for rows.Next() {
		var contentID, oldTitle, filePath string
		var authors []string
		if err := rows.Scan(&contentID, &oldTitle, &filePath, &authors); err != nil {
			return nil, err
		}
		newTitle := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))
		for _, author := range authors {
			suffix := " - " + strings.TrimSpace(author)
			if strings.HasSuffix(strings.ToLower(newTitle), strings.ToLower(suffix)) {
				newTitle = strings.TrimSpace(newTitle[:len(newTitle)-len(suffix)])
				break
			}
		}
		if newTitle == "" || strings.EqualFold(strings.TrimSpace(oldTitle), newTitle) || isSuspiciousEbookTitle(newTitle) {
			continue
		}
		out = append(out, titleRepair{ContentID: contentID, OldTitle: oldTitle, NewTitle: newTitle})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

func suspiciousEbookTitles() []string {
	return []string{"cover", "contents", "content", "title page", "vorwort", "inhalt", "inhaltsverzeichnis", "danksagung", "geleitwort", "de.downmagaz.com", "de.downmagaz.net", "https://downmagaz.net"}
}

func isSuspiciousEbookTitle(title string) bool {
	for _, suspicious := range suspiciousEbookTitles() {
		if strings.EqualFold(strings.TrimSpace(title), suspicious) {
			return true
		}
	}
	return false
}

func loadSuspiciousTitleCounts(ctx context.Context, pool *pgxpool.Pool, out map[string]int) error {
	rows, err := pool.Query(ctx, `
		SELECT lower(btrim(title)), COUNT(*)::int
		FROM media_items
		WHERE type = 'ebook'
		  AND lower(btrim(title)) = ANY($1::text[])
		GROUP BY lower(btrim(title))
		ORDER BY lower(btrim(title))`, suspiciousEbookTitles())
	if err != nil {
		return fmt.Errorf("query suspicious titles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var title string
		var count int
		if err := rows.Scan(&title, &count); err != nil {
			return err
		}
		out[title] = count
	}
	return rows.Err()
}

func applyRepairs(ctx context.Context, pool *pgxpool.Pool, limit int, report *auditReport) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('silo:book-library-repair'))`); err != nil {
		return err
	}
	credits, err := loadInvalidCredits(ctx, tx, limit)
	if err != nil {
		return err
	}
	series, err := loadSeriesRepairs(ctx, tx, limit)
	if err != nil {
		return err
	}
	report.InvalidCredits = credits
	report.SeriesRepairs = series
	titles, err := loadTitleRepairs(ctx, tx, limit)
	if err != nil {
		return err
	}
	report.TitleRepairs = titles
	for _, c := range credits {
		tag, err := tx.Exec(ctx, `
			DELETE FROM item_people ip
			USING media_items mi, people p
			WHERE ip.content_id=$1 AND ip.person_id=$2 AND ip.kind=$3
			  AND mi.content_id=ip.content_id
			  AND lower(btrim(COALESCE(mi.status, ''))) <> 'curated'
			  AND p.id=ip.person_id AND p.name=$4
		`, c.ContentID, c.PersonID, c.Kind, c.Name)
		if err != nil {
			return fmt.Errorf("delete invalid credit %s/%d: %w", c.ContentID, c.PersonID, err)
		}
		report.AppliedInvalidCredits += tag.RowsAffected()
	}
	for _, s := range series {
		if s.Table != "audiobook_series" && s.Table != "ebook_series" {
			return errors.New("invalid series table")
		}
		tag, err := tx.Exec(ctx, `UPDATE `+s.Table+` SET series_name=$2, series_key=$3, updated_at=NOW() WHERE content_id=$1 AND series_name=$4 AND series_key=$5`, s.ContentID, s.NewName, s.NewKey, s.OldName, s.OldKey)
		if err != nil {
			return fmt.Errorf("repair series %s/%s: %w", s.Table, s.ContentID, err)
		}
		report.AppliedSeriesRepairs += tag.RowsAffected()
	}
	for _, repair := range titles {
		tag, err := tx.Exec(ctx, `
			UPDATE media_items
			SET title=$2, sort_title=$4, updated_at=NOW()
			WHERE content_id=$1 AND title=$3
			  AND lower(btrim(COALESCE(status, ''))) <> 'curated'
		`, repair.ContentID, repair.NewTitle, repair.OldTitle, titleutil.DeriveDefaultSortTitle(repair.NewTitle))
		if err != nil {
			return fmt.Errorf("repair ebook title %s: %w", repair.ContentID, err)
		}
		report.AppliedTitleRepairs += tag.RowsAffected()
	}
	return tx.Commit(ctx)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
