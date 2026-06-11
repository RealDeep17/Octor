package discover_watchlist

import (
	"testing"
	"time"

	"github.com/webtor-io/web-ui/models"
)

func TestParseContentType(t *testing.T) {
	tests := []struct {
		input    string
		expected models.ContentType
		wantErr  bool
	}{
		{"movie", models.ContentTypeMovie, false},
		{"adult", models.ContentTypeMovie, false},
		{"porn", models.ContentTypeMovie, false},
		{"jav", models.ContentTypeMovie, false},
		{"series", models.ContentTypeSeries, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := parseContentType(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseContentType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.expected {
			t.Errorf("parseContentType(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestMergeByCreatedAtDesc(t *testing.T) {
	now := time.Now().Unix()
	item1 := models.WatchlistItem{VideoID: "1", CreatedAt: now}
	item2 := models.WatchlistItem{VideoID: "2", CreatedAt: now - 10}
	item3 := models.WatchlistItem{VideoID: "3", CreatedAt: now - 5}
	item4 := models.WatchlistItem{VideoID: "4", CreatedAt: now - 20}

	a := []models.WatchlistItem{item1, item4}
	b := []models.WatchlistItem{item3, item2}

	merged := mergeByCreatedAtDesc(a, b)

	if len(merged) != 4 {
		t.Fatalf("expected 4 merged items, got %d", len(merged))
	}

	expectedOrder := []string{"1", "3", "2", "4"}
	for i, item := range merged {
		if item.VideoID != expectedOrder[i] {
			t.Errorf("at index %d, expected video_id %q, got %q", i, expectedOrder[i], item.VideoID)
		}
	}
}
