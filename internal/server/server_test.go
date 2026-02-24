package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourorg/apidoc/internal/config"
	"github.com/yourorg/apidoc/internal/store"
	"github.com/yourorg/apidoc/pkg/types"
)

func newTestServer(t *testing.T) (*Server, *store.SQLiteStore) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Output.Dir = filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(cfg.Output.Dir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "apidoc.db")
	st, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	srv, err := New(cfg, st)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, st
}

func TestServerSessionsEmpty(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var sessions []types.Session
	if err := json.NewDecoder(rec.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected empty sessions, got %d", len(sessions))
	}
}

func TestServerTrafficAndSessionDetail(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := map[string]any{
		"scenario": "test scenario",
		"logs": []map[string]any{
			{
				"method":                "GET",
				"url":                   "https://example.com/ping",
				"host":                  "example.com",
				"path":                  "/ping",
				"query_params":          map[string][]string{"q": {"1"}},
				"request_headers":       map[string]string{"X-Test": "1"},
				"request_body":          "",
				"content_type":          "application/json",
				"status_code":           200,
				"response_headers":      map[string]string{"Content-Type": "application/json"},
				"response_body":         "{}",
				"response_content_type": "application/json",
				"latency_ms":            12,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/traffic", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var trafficResp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&trafficResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	sessionID := trafficResp["session_id"]
	if sessionID == "" {
		t.Fatalf("missing session_id in response")
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID, nil)
	detailRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(detailRec, detailReq)

	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detailRec.Code)
	}

	var detailResp struct {
		Session *types.Session     `json:"session"`
		Logs    []types.TrafficLog `json:"logs"`
	}
	if err := json.NewDecoder(detailRec.Body).Decode(&detailResp); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detailResp.Session == nil || detailResp.Session.ID != sessionID {
		t.Fatalf("unexpected session: %+v", detailResp.Session)
	}
	if len(detailResp.Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(detailResp.Logs))
	}
	if detailResp.Logs[0].Path != "/ping" {
		t.Fatalf("unexpected log path: %s", detailResp.Logs[0].Path)
	}
}

func TestServerIndexHTML(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	if !bytes.Contains(rec.Body.Bytes(), []byte("apidoc")) {
		t.Fatalf("expected body to contain apidoc")
	}
}
