package emby

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetItemUsesConfiguredUserID(t *testing.T) {
	const userID = "configured-user"
	usersRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Users" {
			usersRequests++
			http.Error(w, "unexpected user discovery", http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/Users/"+userID+"/Items/item-1" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if r.Header.Get("X-Emby-Token") != "api-key" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("Fields") != itemFields {
			http.Error(w, "missing fields", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Id":   "item-1",
			"Type": "Episode",
		})
	}))
	defer server.Close()

	item, err := NewWithUserID(server.URL, "api-key", userID).GetItem(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("GetItem() error = %v", err)
	}
	if item == nil || item["Id"] != "item-1" {
		t.Fatalf("GetItem() = %#v, want item-1", item)
	}
	if usersRequests != 0 {
		t.Fatalf("user discovery requests = %d, want 0", usersRequests)
	}
}

func TestGetItemDiscoversUserIDWhenNotConfigured(t *testing.T) {
	const userID = "discovered-user"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"Id": userID}})
		case "/Users/" + userID + "/Items/item-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "item-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	item, err := New(server.URL, "api-key").GetItem(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("GetItem() error = %v", err)
	}
	if item == nil || item["Id"] != "item-1" {
		t.Fatalf("GetItem() = %#v, want item-1", item)
	}
}
