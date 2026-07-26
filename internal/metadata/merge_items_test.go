package metadata

import "testing"

func TestRebindDeletableStatusesIncludesLegacyBlankStatus(t *testing.T) {
	for _, status := range rebindDeletableStatuses(false) {
		if status == "" {
			return
		}
	}
	t.Fatal("legacy blank status must be mergeable")
}
