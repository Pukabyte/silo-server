package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// coverageQuerier is the read surface the semantic-coverage counts need from a
// database handle. *pgxpool.Pool satisfies it, so the existing callers continue
// to pass their pool unchanged. A later task layers a cached snapshot tracker on
// top of these queries in this same file.
type coverageQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// catalogTypeCoverage is the per-media-type semantic vector coverage: how many
// embed-eligible items exist (Eligible) and how many of those carry a
// current-model embedding (Vectorized). By construction Vectorized <= Eligible
// for every row.
type catalogTypeCoverage struct {
	Type       string
	Eligible   int
	Vectorized int
}

// semanticCoverageEligibleByTypeSQL counts embed-eligible items per media type
// (the coverage denominator). The eligibility predicate is shared with the
// recommendations population via embeddingvectors.ItemEligibilityWhereClause;
// it is inlined here as a literal so the count stays a single round trip.
const semanticCoverageEligibleByTypeSQL = `
SELECT mi.type, COUNT(*) AS eligible
FROM media_items mi
WHERE NOT EXISTS (SELECT 1 FROM manga_chapters mc WHERE mc.chapter_content_id = mi.content_id)
  AND ($1::text[] IS NULL OR mi.type = ANY($1))
  AND (mi.status = 'matched' OR mi.type IN ('audiobook','ebook'))
GROUP BY mi.type`

// semanticCoverageVectorizedByTypeSQL counts embed-eligible items that already
// carry a current-model embedding per media type (the coverage numerator).
// Applying the same eligibility predicate as the denominator guarantees the
// numerator is a subset of the denominator, so a stale embedding left on a
// now-unmatched item can never push a per-type ratio above 1.
const semanticCoverageVectorizedByTypeSQL = `
SELECT mi.type, COUNT(*) AS vectorized
FROM media_item_embeddings e
JOIN media_items mi ON mi.content_id = e.media_item_id
WHERE NOT EXISTS (SELECT 1 FROM manga_chapters mc WHERE mc.chapter_content_id = mi.content_id)
  AND ($1::text[] IS NULL OR mi.type = ANY($1))
  AND (mi.status = 'matched' OR mi.type IN ('audiobook','ebook'))
  AND ($2 = '' OR e.model = $2)
GROUP BY mi.type`

// catalogSemanticCoverageByType returns per-type eligible and vectorized counts
// for the requested item types (nil/empty => all types) and embedding model
// ("" => count every model). The numerator and denominator share the
// embed-eligibility predicate, so every returned row satisfies
// Vectorized <= Eligible.
func catalogSemanticCoverageByType(ctx context.Context, q coverageQuerier, itemTypes []string, model string) ([]catalogTypeCoverage, error) {
	if q == nil {
		return nil, nil
	}
	typeFilter := normalizeCatalogSearchItemTypes(itemTypes)
	var typeArg any
	if len(typeFilter) > 0 {
		typeArg = typeFilter
	}

	coverage := make(map[string]*catalogTypeCoverage)
	order := make([]string, 0)
	upsert := func(mediaType string) *catalogTypeCoverage {
		if row, ok := coverage[mediaType]; ok {
			return row
		}
		row := &catalogTypeCoverage{Type: mediaType}
		coverage[mediaType] = row
		order = append(order, mediaType)
		return row
	}

	eligibleRows, err := q.Query(ctx, semanticCoverageEligibleByTypeSQL, typeArg)
	if err != nil {
		return nil, fmt.Errorf("query semantic coverage eligible counts: %w", err)
	}
	func() {
		defer eligibleRows.Close()
		for eligibleRows.Next() {
			var mediaType string
			var eligible int
			if err = eligibleRows.Scan(&mediaType, &eligible); err != nil {
				return
			}
			upsert(mediaType).Eligible = eligible
		}
		err = eligibleRows.Err()
	}()
	if err != nil {
		return nil, fmt.Errorf("scan semantic coverage eligible counts: %w", err)
	}

	vectorRows, err := q.Query(ctx, semanticCoverageVectorizedByTypeSQL, typeArg, model)
	if err != nil {
		return nil, fmt.Errorf("query semantic coverage vectorized counts: %w", err)
	}
	func() {
		defer vectorRows.Close()
		for vectorRows.Next() {
			var mediaType string
			var vectorized int
			if err = vectorRows.Scan(&mediaType, &vectorized); err != nil {
				return
			}
			upsert(mediaType).Vectorized = vectorized
		}
		err = vectorRows.Err()
	}()
	if err != nil {
		return nil, fmt.Errorf("scan semantic coverage vectorized counts: %w", err)
	}

	out := make([]catalogTypeCoverage, 0, len(order))
	for _, mediaType := range order {
		out = append(out, *coverage[mediaType])
	}
	return out, nil
}
