package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"embytool/internal/config"
	"embytool/internal/cover"
	"embytool/internal/fonts"
)

func newFakeEmbyServer(t *testing.T) *httptest.Server {
	t.Helper()
	embyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users":
			writeJSON(w, http.StatusOK, []map[string]any{{
				"Id":     "user-1",
				"Policy": map[string]any{"IsAdministrator": true},
			}})
		case "/Users/user-1/Views":
			writeJSON(w, http.StatusOK, map[string]any{
				"Items": []map[string]any{{
					"Id":             "11",
					"Name":           "剧集",
					"CollectionType": "tvshows",
				}},
			})
		default:
			writeJSON(w, http.StatusOK, map[string]any{"Items": []map[string]any{}})
		}
	}))
	t.Cleanup(embyServer.Close)
	return embyServer
}

func newGenerationTestServer(embyServerURL string) *Server {
	cfg := config.Default()
	cfg.EmbyServerURL = embyServerURL
	cfg.EmbyAPIKey = "test-key"
	return &Server{
		cfg:      cfg,
		coverSvc: cover.NewService(fonts.NewCache()),
	}
}

func waitForGenerationJob(t *testing.T, s *Server) generationStatusResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var status generationStatusResponse
	for time.Now().Before(deadline) {
		s.genMu.Lock()
		status = s.genJob.snapshot()
		s.genMu.Unlock()
		if !status.Running {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("generation job still running after timeout")
	return status
}

func TestGenerationJobRunsInBackground(t *testing.T) {
	s := newGenerationTestServer(newFakeEmbyServer(t).URL)

	jobID, err := s.startGenerationJob([]string{"11"}, false)
	if err != nil {
		t.Fatalf("startGenerationJob() error = %v", err)
	}
	if jobID == "" {
		t.Fatal("startGenerationJob() jobID is empty")
	}

	status := waitForGenerationJob(t, s)
	if status.Status != "completed" {
		t.Fatalf("generation job status = %q, message = %q", status.Status, status.Message)
	}
	if len(status.Results) != 1 {
		t.Fatalf("generation job results = %d, want 1", len(status.Results))
	}
	if status.Results[0].Library != "剧集" {
		t.Fatalf("generation job result library = %q, want 剧集", status.Results[0].Library)
	}
}

func TestGenerationEndpointsStartAndReportJob(t *testing.T) {
	s := newGenerationTestServer(newFakeEmbyServer(t).URL)
	api := httptest.NewServer(s.mux())
	defer api.Close()

	resp, err := http.Post(api.URL+"/api/libraries/generate", "application/json", bytes.NewBufferString(`{"library_ids":["11"]}`))
	if err != nil {
		t.Fatalf("POST /api/libraries/generate error = %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", resp.StatusCode, body)
	}
	var start struct {
		OK    bool   `json:"ok"`
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(body, &start); err != nil {
		t.Fatalf("decode start response error = %v", err)
	}
	if !start.OK || start.JobID == "" {
		t.Fatalf("start response = %+v", start)
	}

	deadline := time.Now().Add(5 * time.Second)
	var status struct {
		Status  string `json:"status"`
		Running bool   `json:"running"`
	}
	for time.Now().Before(deadline) {
		resp, err := http.Get(api.URL + "/api/generation/status")
		if err != nil {
			t.Fatalf("GET /api/generation/status error = %v", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read status response error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status HTTP status = %d, body = %s", resp.StatusCode, body)
		}
		if err := json.Unmarshal(body, &status); err != nil {
			t.Fatalf("decode status response error = %v", err)
		}
		if !status.Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.Running {
		t.Fatal("generation job still running after timeout")
	}
	if status.Status != "completed" {
		t.Fatalf("generation job status = %q", status.Status)
	}
}
