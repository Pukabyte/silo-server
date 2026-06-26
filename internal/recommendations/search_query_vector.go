package recommendations

import (
	"context"
	"fmt"
	"strings"
)

// EmbedSearchQuery generates a canonical recommendation-space vector for a
// free-text catalog search query.
func (e *Engine) EmbedSearchQuery(ctx context.Context, query string) ([]float32, error) {
	if e == nil || e.embClient == nil {
		return nil, fmt.Errorf("recommendation embedding client is not configured")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if strings.TrimSpace(e.cfg.EmbeddingBaseURL) == "" {
		return nil, fmt.Errorf("recommendation embedding base URL is not configured")
	}
	if strings.TrimSpace(e.cfg.EmbeddingModel) == "" {
		return nil, fmt.Errorf("recommendation embedding model is not configured")
	}
	if err := e.ensureEmbeddingLockConfig(ctx); err != nil {
		return nil, err
	}
	vectors, err := e.embClient.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("embedding API returned no query vector")
	}
	if err := e.ensureEmbeddingLock(ctx, vectors[0]); err != nil {
		return nil, err
	}
	return ensureCanonicalDimensions(vectors[0])
}
