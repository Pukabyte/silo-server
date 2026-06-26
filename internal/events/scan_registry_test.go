package events

import (
	"strconv"
	"testing"
)

func TestScanRegistryListActiveLimit(t *testing.T) {
	registry := NewScanRegistry()
	for i := 0; i < 3; i++ {
		registry.Upsert(ScanRun{ID: "active-" + strconv.Itoa(i), Status: "accepted"})
	}
	registry.Upsert(ScanRun{ID: "completed", Status: "completed"})

	limited := registry.ListActiveLimit(2)
	if len(limited) != 2 {
		t.Fatalf("limited active runs = %d, want 2", len(limited))
	}
	for _, run := range limited {
		if run.Status != "accepted" && run.Status != "running" {
			t.Fatalf("limited run should be active, got %+v", run)
		}
	}

	all := registry.ListActiveLimit(0)
	if len(all) != 3 {
		t.Fatalf("all active runs = %d, want 3", len(all))
	}
}
