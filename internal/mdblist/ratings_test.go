package mdblist

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRatingsByIMDBHappyPath(t *testing.T) {
	var capturedPath, capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ratings":[{"source":"imdb","value":8.5},{"source":"tomatoes","value":91},{"source":"tomatoesaudience","value":88}]}`))
	}))
	defer srv.Close()

	c := NewClient("secret-key", srv.Client())
	c.ratingsBaseURL = srv.URL

	got, err := c.RatingsByIMDB(context.Background(), "tt0111161")
	if err != nil {
		t.Fatalf("RatingsByIMDB returned error: %v", err)
	}
	if capturedPath != "/" {
		t.Errorf("path = %q, want /", capturedPath)
	}
	if !strings.Contains(capturedQuery, "apikey=secret-key") {
		t.Errorf("apikey missing from query: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "i=tt0111161") {
		t.Errorf("imdb id missing from query: %q", capturedQuery)
	}
	if got.RTCritic == nil || *got.RTCritic != 91 {
		t.Errorf("RTCritic = %v, want 91", got.RTCritic)
	}
	if got.RTAudience == nil || *got.RTAudience != 88 {
		t.Errorf("RTAudience = %v, want 88", got.RTAudience)
	}
	if got.IMDB == nil || *got.IMDB != 8.5 {
		t.Errorf("IMDB = %v, want 8.5", got.IMDB)
	}
}

func TestRatingsByIMDBNullValuesLeftNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ratings":[{"source":"tomatoes","value":null},{"source":"tomatoesaudience","value":72}]}`))
	}))
	defer srv.Close()

	c := NewClient("k", srv.Client())
	c.ratingsBaseURL = srv.URL

	got, err := c.RatingsByIMDB(context.Background(), "tt0111161")
	if err != nil {
		t.Fatalf("RatingsByIMDB returned error: %v", err)
	}
	if got.RTCritic != nil {
		t.Errorf("RTCritic = %v, want nil", *got.RTCritic)
	}
	if got.RTAudience == nil || *got.RTAudience != 72 {
		t.Errorf("RTAudience = %v, want 72", got.RTAudience)
	}
}

func TestRatingsByIMDBNotConfigured(t *testing.T) {
	c := NewClient("", nil)
	_, err := c.RatingsByIMDB(context.Background(), "tt0111161")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestRatingsByIMDBRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient("k", srv.Client())
	c.ratingsBaseURL = srv.URL

	if _, err := c.RatingsByIMDB(context.Background(), "tt0111161"); err == nil {
		t.Fatal("expected rate limit error, got nil")
	}
}
