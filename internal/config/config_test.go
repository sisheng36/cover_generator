package config

import "testing"

func TestNormalizeEmbyUserID(t *testing.T) {
	cfg := Normalize(map[string]any{
		"emby_user_id": "  user-1  ",
	})
	if cfg.EmbyUserID != "user-1" {
		t.Fatalf("EmbyUserID = %q, want %q", cfg.EmbyUserID, "user-1")
	}

	payload := cfg.Map()
	if got, ok := payload["emby_user_id"].(string); !ok || got != "user-1" {
		t.Fatalf("mapped emby_user_id = %#v, want %q", payload["emby_user_id"], "user-1")
	}
}
