package models

import (
	"testing"
)

func TestNormalizeStremioSettings(t *testing.T) {
	t.Run("nil settings", func(t *testing.T) {
		res := NormalizeStremioSettings(nil)
		if res == nil {
			t.Fatal("expected non-nil settings")
		}
		if len(res.PreferredResolutions) != 5 {
			t.Fatalf("expected 5 resolutions, got %d", len(res.PreferredResolutions))
		}
		if res.PreferredResolutions[0].Resolution != "8k" || res.PreferredResolutions[0].Enabled {
			t.Errorf("expected disabled 8k first, got %+v", res.PreferredResolutions[0])
		}
	})

	t.Run("missing 8k", func(t *testing.T) {
		input := &StremioSettingsData{
			PreferredResolutions: []ResolutionSetting{
				{Resolution: "4k", Enabled: true},
				{Resolution: "1080p", Enabled: false},
			},
		}
		res := NormalizeStremioSettings(input)
		if len(res.PreferredResolutions) != 5 { // 8k added, 4k kept, 1080p kept, 720p added, other added
			t.Fatalf("expected 5 resolutions, got %d", len(res.PreferredResolutions))
		}
		if res.PreferredResolutions[0].Resolution != "8k" || res.PreferredResolutions[0].Enabled {
			t.Errorf("expected disabled 8k first, got %+v", res.PreferredResolutions[0])
		}
		if res.PreferredResolutions[1].Resolution != "4k" || !res.PreferredResolutions[1].Enabled {
			t.Errorf("expected enabled 4k second, got %+v", res.PreferredResolutions[1])
		}
		if res.PreferredResolutions[2].Resolution != "1080p" || res.PreferredResolutions[2].Enabled {
			t.Errorf("expected disabled 1080p third, got %+v", res.PreferredResolutions[2])
		}
	})

	t.Run("already has 8k", func(t *testing.T) {
		input := &StremioSettingsData{
			PreferredResolutions: []ResolutionSetting{
				{Resolution: "8k", Enabled: true},
				{Resolution: "4k", Enabled: true},
			},
		}
		res := NormalizeStremioSettings(input)
		// It shouldn't add 8k again
		if len(res.PreferredResolutions) != 5 {
			t.Fatalf("expected 5 resolutions, got %d", len(res.PreferredResolutions))
		}
		if res.PreferredResolutions[0].Resolution != "8k" || !res.PreferredResolutions[0].Enabled {
			t.Errorf("expected enabled 8k first, got %+v", res.PreferredResolutions[0])
		}
	})
}
