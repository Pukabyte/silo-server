package catalog

import (
	"context"
	"strings"
	"testing"
)

func TestMeilisearchWaitTaskRejectsZeroTaskUID(t *testing.T) {
	err := (&meilisearchClient{}).WaitTask(context.Background(), 0)
	if err == nil {
		t.Fatal("WaitTask(0) returned nil")
	}
	if !strings.Contains(err.Error(), "task UID is required") {
		t.Fatalf("WaitTask(0) error = %v", err)
	}
}
