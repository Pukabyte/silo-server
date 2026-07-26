package metadata

import (
	"strings"
	"testing"
)

func TestPersonalCollectionMergePreservesSubItemIdentity(t *testing.T) {
	t.Parallel()
	for _, step := range mediaItemMergeSteps {
		if step.name != "merge personal collection items" {
			continue
		}
		if !strings.Contains(step.sql, "sub_item_id") || !strings.Contains(step.sql, "ON CONFLICT (user_id, collection_id, media_item_id, sub_item_id)") {
			t.Fatalf("personal collection merge omits sub-item identity: %s", step.sql)
		}
		return
	}
	t.Fatal("personal collection merge step not found")
}
