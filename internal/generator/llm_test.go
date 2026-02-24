package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yourorg/apidoc/pkg/types"
)

func TestChatJSONStripsMarkdown(t *testing.T) {
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "```json\n{\"scenario\":\"s\",\"call_chain\":[],\"endpoints\":[]}\n```"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Model: "gpt-4o"}
	var out types.GeneratedDoc
	if err := client.ChatJSON("sys", "user", &out); err != nil {
		t.Fatalf("ChatJSON error: %v", err)
	}
	if out.Scenario != "s" {
		t.Fatalf("expected scenario 's', got %q", out.Scenario)
	}
	if atomic.LoadInt32(&hit) != 1 {
		t.Fatalf("expected 1 request, got %d", hit)
	}
}

func TestChatRetriesOn5xx(t *testing.T) {
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&hit, 1)
		if count == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "{\"scenario\":\"ok\",\"call_chain\":[],\"endpoints\":[]}"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	origSleep := sleepFn
	sleepFn = func(time.Duration) {}
	defer func() { sleepFn = origSleep }()

	client := &Client{BaseURL: srv.URL, Model: "gpt-4o"}
	var out types.GeneratedDoc
	if err := client.ChatJSON("sys", "user", &out); err != nil {
		t.Fatalf("ChatJSON error: %v", err)
	}
	if out.Scenario != "ok" {
		t.Fatalf("expected scenario 'ok', got %q", out.Scenario)
	}
	if atomic.LoadInt32(&hit) != 2 {
		t.Fatalf("expected 2 requests, got %d", hit)
	}
}
