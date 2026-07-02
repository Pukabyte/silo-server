package mdblist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// TitleRatings holds the ratings MDBList reports for a single title. Pointers
// distinguish "absent" from a genuine zero score.
type TitleRatings struct {
	IMDB       *float64
	RTCritic   *float64
	RTAudience *float64
}

// ratingsResponse mirrors the relevant portion of the MDBList title lookup
// response (GET /?apikey=...&i=<imdbID>).
type ratingsResponse struct {
	Ratings []struct {
		Source string   `json:"source"`
		Value  *float64 `json:"value"`
	} `json:"ratings"`
}

// RatingsByIMDB fetches ratings for an IMDb id (e.g. "tt0111161") from MDBList.
// The RTCritic/RTAudience scores are on a 0-100 scale. Returns ErrNotConfigured
// when no apikey is set. Ratings the upstream reports as null are left nil.
func (c *Client) RatingsByIMDB(ctx context.Context, imdbID string) (*TitleRatings, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	imdbID = strings.TrimSpace(imdbID)
	if imdbID == "" {
		return nil, fmt.Errorf("imdbID is required")
	}
	q := url.Values{}
	q.Set("apikey", c.currentAPIKey())
	q.Set("i", imdbID)
	u := c.ratingsBaseURL + "/?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating mdblist request: %w", err)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling mdblist: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("mdblist rejected apikey (status %d)", res.StatusCode)
	}
	if res.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("mdblist rate limit exceeded")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("mdblist request failed with status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading mdblist response: %w", err)
	}
	var parsed ratingsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing mdblist response: %w", err)
	}
	out := &TitleRatings{}
	for _, r := range parsed.Ratings {
		if r.Value == nil {
			continue
		}
		switch r.Source {
		case "imdb":
			out.IMDB = r.Value
		case "tomatoes":
			out.RTCritic = r.Value
		case "tomatoesaudience":
			out.RTAudience = r.Value
		}
	}
	return out, nil
}
