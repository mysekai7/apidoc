package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/yourorg/apidoc/internal/har"
	"github.com/yourorg/apidoc/internal/store"
	"github.com/yourorg/apidoc/pkg/types"
)

func TestGenerateIntegrationCacheResumeNoCache(t *testing.T) {
	// Find repo root (two levels up from internal/generator)
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	workDir := t.TempDir()

	dbPath := filepath.Join(workDir, "apidoc.db")
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer s.Close()

	sess, err := s.CreateSession("har", "sample", "api.example.com")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	logs, err := har.Parse(filepath.Join(repoRoot, "testdata", "sample.har"))
	if err != nil {
		t.Fatalf("parse har: %v", err)
	}

	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&hit, 1)
		var content string
		if count == 1 {
			content = `{"scenario":"sample","call_chain":[{"seq":1,"method":"GET","path":"/v1/users","description":"list"}],"endpoints":[{"method":"GET","path":"/v1/users","summary":"list","description":"","responses":[{"status_code":200,"description":"ok"}]}]}`
		} else {
			content = `{"scenario":"sample","call_chain":[{"seq":1,"method":"GET","path":"/v1/users","description":"list"},{"seq":2,"method":"POST","path":"/v1/login","description":"login"}],"endpoints":[{"method":"POST","path":"/v1/login","summary":"login","description":"","responses":[{"status_code":200,"description":"ok"}]}]}`
		}
		resp := map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	llmCfg := LLMConfig{BaseURL: srv.URL, Model: "gpt-4o", MaxTokens: 1}

	doc, err := Generate(sess, logs, llmCfg, s, nil, false, false)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(doc.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(doc.Endpoints))
	}
	if len(doc.CallChain) != 2 {
		t.Fatalf("expected call chain from last batch")
	}
	caches, err := s.GetBatchCaches(sess.ID)
	if err != nil {
		t.Fatalf("get caches: %v", err)
	}
	if len(caches) != 2 {
		t.Fatalf("expected 2 caches, got %d", len(caches))
	}

	// resume should use cache
	if _, err := Generate(sess, logs, llmCfg, s, nil, false, true); err != nil {
		t.Fatalf("resume generate: %v", err)
	}
	if atomic.LoadInt32(&hit) != 2 {
		t.Fatalf("expected no extra llm calls on resume, got %d", hit)
	}

	// no-cache should call llm again
	if _, err := Generate(sess, logs, llmCfg, s, nil, true, false); err != nil {
		t.Fatalf("no-cache generate: %v", err)
	}
	if atomic.LoadInt32(&hit) != 4 {
		t.Fatalf("expected llm calls after no-cache, got %d", hit)
	}
}

func TestGenerateResumeFailedBatch(t *testing.T) {
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "apidoc.db")
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer s.Close()

	sess, err := s.CreateSession("har", "sample", "api.example.com")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	logs := []types.TrafficLog{{Method: "GET", Path: "/v1/users"}, {Method: "POST", Path: "/v1/login"}}

	okDoc := `{"scenario":"sample","call_chain":[],"endpoints":[{"method":"GET","path":"/v1/users","summary":"list","description":"","responses":[{"status_code":200,"description":"ok"}]}]}`
	if err := s.SaveBatchCache(&types.LLMCache{SessionID: sess.ID, BatchIndex: 0, BatchKey: "/v1/users", Status: "ok", RawOutput: okDoc, Model: "gpt-4o"}); err != nil {
		t.Fatalf("save ok cache: %v", err)
	}
	if err := s.SaveBatchCache(&types.LLMCache{SessionID: sess.ID, BatchIndex: 1, BatchKey: "/v1/login", Status: "failed", ErrorMsg: "boom", Model: "gpt-4o"}); err != nil {
		t.Fatalf("save failed cache: %v", err)
	}

	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		content := `{"scenario":"sample","call_chain":[{"seq":1,"method":"GET","path":"/v1/users","description":"list"},{"seq":2,"method":"POST","path":"/v1/login","description":"login"}],"endpoints":[{"method":"POST","path":"/v1/login","summary":"login","description":"","responses":[{"status_code":200,"description":"ok"}]}]}`
		resp := map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	llmCfg := LLMConfig{BaseURL: srv.URL, Model: "gpt-4o", MaxTokens: 1}
	if _, err := Generate(sess, logs, llmCfg, s, nil, false, true); err != nil {
		t.Fatalf("generate resume failed batch: %v", err)
	}
	if atomic.LoadInt32(&hit) != 1 {
		t.Fatalf("expected 1 llm call for failed batch, got %d", hit)
	}
}
