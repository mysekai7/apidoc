package server

import (
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/yourorg/apidoc/internal/config"
	"github.com/yourorg/apidoc/internal/generator"
	"github.com/yourorg/apidoc/internal/store"
	"github.com/yourorg/apidoc/pkg/types"
)

var (
	//go:embed ui.html
	uiHTML string

	uiTemplate = template.Must(template.New("ui").Parse(uiHTML))
)

// Server wraps the preview UI and API handlers.
type Server struct {
	cfg   *config.Config
	store store.Store
	mux   *http.ServeMux
}

type uiData struct {
	SessionID string
}

// New constructs a new Server with routes registered.
func New(cfg *config.Config, st store.Store) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}
	if st == nil {
		return nil, errors.New("store is nil")
	}

	srv := &Server{
		cfg:   cfg,
		store: st,
		mux:   http.NewServeMux(),
	}
	srv.registerRoutes()
	return srv, nil
}

// Handler returns the http handler.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// ListenAndServe starts the server on addr.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) registerRoutes() {
	// Static file server for output docs.
	s.mux.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.Dir(s.cfg.Output.Dir))))

	// UI routes.
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/session/", s.handleSessionPage)

	// API routes.
	s.mux.HandleFunc("/api/sessions", s.handleSessions)
	s.mux.HandleFunc("/api/sessions/", s.handleSessionRoutes)
	s.mux.HandleFunc("/api/traffic", s.handleTraffic)
	s.mux.HandleFunc("/api/generate", s.handleGenerate)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.renderUI(w, "")
}

func (s *Server) handleSessionPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, tail, ok := splitPath(r.URL.Path, "/session/")
	if !ok || id == "" || tail != "" {
		http.NotFound(w, r)
		return
	}
	s.renderUI(w, id)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessions, err := s.store.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleSessionRoutes(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := splitPath(r.URL.Path, "/api/sessions/")
	if !ok || id == "" {
		http.NotFound(w, r)
		return
	}
	if tail == "doc" {
		s.handleSessionDoc(w, r, id)
		return
	}
	if tail != "" {
		http.NotFound(w, r)
		return
	}
	s.handleSessionDetail(w, r, id)
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.store.GetSession(id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	logs, err := s.store.GetLogs(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := struct {
		Session *types.Session    `json:"session"`
		Logs    []types.TrafficLog `json:"logs"`
	}{
		Session: sess,
		Logs:    logs,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSessionDoc(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.store.GetSession(id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	caches, err := s.store.GetBatchCaches(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	docs := make([]*types.GeneratedDoc, 0)
	for _, cache := range caches {
		if cache.Status != "ok" || cache.RawOutput == "" {
			continue
		}
		var doc types.GeneratedDoc
		if err := json.Unmarshal([]byte(cache.RawOutput), &doc); err != nil {
			continue
		}
		if doc.Scenario == "" {
			doc.Scenario = sess.Scenario
		}
		docs = append(docs, &doc)
	}
	if len(docs) == 0 {
		http.Error(w, "doc not found", http.StatusNotFound)
		return
	}
	merged := generator.MergeDocs(docs)
	writeJSON(w, http.StatusOK, merged)
}

func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	setCORS(w, s.cfg.Server.CORSExtensionID)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Scenario string `json:"scenario"`
		Logs     []struct {
			Method              string              `json:"method"`
			URL                 string              `json:"url"`
			Host                string              `json:"host"`
			Path                string              `json:"path"`
			QueryParams         map[string][]string `json:"query_params"`
			RequestHeaders      map[string]string   `json:"request_headers"`
			RequestBody         string              `json:"request_body"`
			ContentType         string              `json:"content_type"`
			StatusCode          int                 `json:"status_code"`
			ResponseHeaders     map[string]string   `json:"response_headers"`
			ResponseBody        string              `json:"response_body"`
			ResponseContentType string              `json:"response_content_type"`
			LatencyMs           int64               `json:"latency_ms"`
		} `json:"logs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	logs := make([]types.TrafficLog, 0, len(req.Logs))
	now := time.Now().UTC()
	for i, l := range req.Logs {
		logs = append(logs, types.TrafficLog{
			Seq:                 i + 1,
			Method:              l.Method,
			Host:                l.Host,
			Path:                l.Path,
			QueryParams:         l.QueryParams,
			RequestHeaders:      l.RequestHeaders,
			RequestBody:         l.RequestBody,
			ContentType:         l.ContentType,
			StatusCode:          l.StatusCode,
			ResponseHeaders:     l.ResponseHeaders,
			ResponseBody:        l.ResponseBody,
			ResponseContentType: l.ResponseContentType,
			LatencyMs:           l.LatencyMs,
			Timestamp:           now,
		})
	}

	host := "unknown"
	if len(req.Logs) > 0 && req.Logs[0].Host != "" {
		host = req.Logs[0].Host
	}
	sess, err := s.store.CreateSession("extension", req.Scenario, host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.SaveLogs(sess.ID, logs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"session_id": sess.ID, "status": "imported"})
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	setCORS(w, s.cfg.Server.CORSExtensionID)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}
	sess, err := s.store.GetSession(req.SessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	logs, err := s.store.GetLogs(sess.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	doc, err := generator.Generate(sess, logs, s.cfg.LLM, s.store, nil, false, false)
	if err != nil {
		http.Error(w, "generate failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) renderUI(w http.ResponseWriter, sessionID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = uiTemplate.Execute(w, uiData{SessionID: sessionID})
}

func splitPath(fullPath, prefix string) (string, string, bool) {
	if !strings.HasPrefix(fullPath, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(fullPath, prefix)
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	id := parts[0]
	tail := ""
	if len(parts) > 1 {
		tail = strings.Join(parts[1:], "/")
	}
	return id, tail, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func setCORS(w http.ResponseWriter, extensionID string) {
	origin := "*"
	if extensionID != "" {
		origin = "chrome-extension://" + extensionID
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
