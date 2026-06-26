package catalogseed

import (
	"reflect"
	"testing"
)

func TestCatalogSeedSearchUpsertIDsIncludesChangedItemsAndEmbeddings(t *testing.T) {
	itemStates := map[string]bool{
		"movie-1":  true,
		"movie-2":  false,
		"series-1": true,
	}
	embeddings := []EmbeddingRecord{
		{MediaItemID: " movie-3 "},
		{MediaItemID: "movie-1"},
		{MediaItemID: ""},
	}

	got := catalogSeedSearchUpsertIDs(itemStates, embeddings)
	want := []string{"movie-1", "movie-3", "series-1"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalogSeedSearchUpsertIDs = %#v, want %#v", got, want)
	}
}
