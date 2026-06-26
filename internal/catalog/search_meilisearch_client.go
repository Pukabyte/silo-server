package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type meilisearchClient struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
}

type meilisearchHTTPError struct {
	StatusCode int
	Message    string
	Code       string
}

func (e *meilisearchHTTPError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("meilisearch HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("meilisearch HTTP %d", e.StatusCode)
}

type meilisearchTask struct {
	TaskUID  int64  `json:"taskUid"`
	IndexUID string `json:"indexUid"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Error    *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
		Type    string `json:"type"`
		Link    string `json:"link"`
	} `json:"error"`
}

type meilisearchSearchRequest struct {
	Query                string                    `json:"q"`
	Offset               int                       `json:"offset"`
	Limit                int                       `json:"limit"`
	Filter               string                    `json:"filter,omitempty"`
	AttributesToRetrieve []string                  `json:"attributesToRetrieve"`
	MatchingStrategy     string                    `json:"matchingStrategy,omitempty"`
	Vector               []float32                 `json:"vector,omitempty"`
	Hybrid               *meilisearchHybridRequest `json:"hybrid,omitempty"`
}

type meilisearchHybridRequest struct {
	Embedder      string  `json:"embedder"`
	SemanticRatio float64 `json:"semanticRatio"`
}

type meilisearchSearchHit struct {
	ContentID string `json:"content_id"`
}

type meilisearchSearchResponse struct {
	Hits               []meilisearchSearchHit `json:"hits"`
	Offset             int                    `json:"offset"`
	Limit              int                    `json:"limit"`
	EstimatedTotalHits int                    `json:"estimatedTotalHits"`
	ProcessingTimeMS   int                    `json:"processingTimeMs"`
	Query              string                 `json:"query"`
}

type meilisearchStatsResponse struct {
	NumberOfDocuments int `json:"numberOfDocuments"`
}

const (
	defaultMeilisearchTaskWaitTimeout = 5 * time.Minute
	meilisearchTaskPollInterval       = time.Second
)

func newMeilisearchClient(rawURL, apiKey string, timeout time.Duration) (*meilisearchClient, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("meilisearch URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing meilisearch URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("meilisearch URL must include scheme and host")
	}
	if timeout <= 0 {
		timeout = time.Duration(DefaultMeilisearchTimeoutMS) * time.Millisecond
	}
	return &meilisearchClient{
		baseURL: parsed,
		apiKey:  strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (c *meilisearchClient) Health(ctx context.Context) error {
	var out struct {
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, "/health", nil, &out); err != nil {
		return err
	}
	if out.Status != "available" {
		return fmt.Errorf("meilisearch health status %q", out.Status)
	}
	return nil
}

func (c *meilisearchClient) CreateIndex(ctx context.Context, uid string) (int64, error) {
	var task meilisearchTask
	err := c.do(ctx, http.MethodPost, "/indexes", map[string]string{
		"uid":        uid,
		"primaryKey": "content_id",
	}, &task)
	if err != nil {
		var httpErr *meilisearchHTTPError
		if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusConflict || httpErr.Code == "index_already_exists") {
			return 0, nil
		}
		return 0, err
	}
	return task.TaskUID, nil
}

func (c *meilisearchClient) UpdateSettings(ctx context.Context, uid string, settings map[string]any) (int64, error) {
	var task meilisearchTask
	if err := c.do(ctx, http.MethodPatch, "/indexes/"+url.PathEscape(uid)+"/settings", settings, &task); err != nil {
		return 0, err
	}
	return task.TaskUID, nil
}

func (c *meilisearchClient) Search(ctx context.Context, uid string, req meilisearchSearchRequest) (meilisearchSearchResponse, error) {
	var out meilisearchSearchResponse
	err := c.do(ctx, http.MethodPost, "/indexes/"+url.PathEscape(uid)+"/search", req, &out)
	return out, err
}

func (c *meilisearchClient) AddDocuments(ctx context.Context, uid string, docs []catalogSearchDocument) (int64, error) {
	if len(docs) == 0 {
		return 0, nil
	}
	var task meilisearchTask
	if err := c.do(ctx, http.MethodPost, "/indexes/"+url.PathEscape(uid)+"/documents", docs, &task); err != nil {
		return 0, err
	}
	return task.TaskUID, nil
}

func (c *meilisearchClient) DeleteDocuments(ctx context.Context, uid string, ids []string) (int64, error) {
	ids = compactNonEmptyStrings(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	var task meilisearchTask
	if err := c.do(ctx, http.MethodPost, "/indexes/"+url.PathEscape(uid)+"/documents/delete-batch", ids, &task); err != nil {
		return 0, err
	}
	return task.TaskUID, nil
}

func (c *meilisearchClient) Stats(ctx context.Context, uid string) (int, error) {
	var out meilisearchStatsResponse
	err := c.do(ctx, http.MethodGet, "/indexes/"+url.PathEscape(uid)+"/stats", nil, &out)
	return out.NumberOfDocuments, err
}

func (c *meilisearchClient) WaitTask(ctx context.Context, taskUID int64) error {
	if taskUID == 0 {
		return nil
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultMeilisearchTaskWaitTimeout)
		defer cancel()
	}
	ticker := time.NewTicker(meilisearchTaskPollInterval)
	defer ticker.Stop()
	for {
		var task meilisearchTask
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/tasks/%d", taskUID), nil, &task); err != nil {
			return err
		}
		switch task.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			if task.Error != nil && task.Error.Message != "" {
				return fmt.Errorf("meilisearch task %d %s: %s", taskUID, task.Status, task.Error.Message)
			}
			return fmt.Errorf("meilisearch task %d %s", taskUID, task.Status)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *meilisearchClient) do(ctx context.Context, method, endpoint string, body any, out any) error {
	if c == nil || c.baseURL == nil || c.httpClient == nil {
		return fmt.Errorf("meilisearch client is not configured")
	}
	reqURL := *c.baseURL
	reqURL.Path = path.Join(c.baseURL.Path, endpoint)
	if strings.HasSuffix(endpoint, "/") && !strings.HasSuffix(reqURL.Path, "/") {
		reqURL.Path += "/"
	}

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		httpErr := &meilisearchHTTPError{StatusCode: resp.StatusCode}
		var errBody struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		if data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024)); readErr == nil && len(data) > 0 {
			if json.Unmarshal(data, &errBody) == nil {
				httpErr.Message = errBody.Message
				httpErr.Code = errBody.Code
			} else {
				httpErr.Message = strings.TrimSpace(string(data))
			}
		}
		return httpErr
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	return nil
}

func compactNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
