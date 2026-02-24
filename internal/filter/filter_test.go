package filter

import (
	"testing"

	"github.com/yourorg/apidoc/pkg/types"
)

func TestApplyFiltersBasic(t *testing.T) {
	cfg := FilterConfig{
		IgnoreExtensions:   []string{".js", ".css", ".png"},
		IgnoreContentTypes: []string{"text/html", "image/*"},
		IgnorePaths:        []string{"/static/", "/assets/", "/favicon"},
	}
	logs := []types.TrafficLog{
		{Method: "OPTIONS", Path: "/api/ping", StatusCode: 204},
		{Method: "GET", Path: "/static/app.js", ResponseContentType: "application/javascript", StatusCode: 200},
		{Method: "GET", Path: "/index", ResponseContentType: "text/html; charset=utf-8", StatusCode: 200},
		{Method: "GET", Path: "/assets/logo.png", ResponseContentType: "image/png", StatusCode: 200},
		{Method: "GET", Path: "/api/data", ResponseContentType: "application/json", StatusCode: 200},
	}

	out := Apply(logs, cfg)
	if len(out) != 1 {
		t.Fatalf("expected 1 log, got %d", len(out))
	}
	if out[0].Path != "/api/data" {
		t.Fatalf("expected /api/data, got %s", out[0].Path)
	}
}

func TestApplyMergeIdenticalRequests(t *testing.T) {
	cfg := FilterConfig{}
	logs := []types.TrafficLog{
		{Method: "GET", Path: "/api/users", QueryParams: map[string][]string{"id": {"1"}}, StatusCode: 200},
		{Method: "GET", Path: "/api/users", QueryParams: map[string][]string{"id": {"1"}}, StatusCode: 200},
		{Method: "GET", Path: "/api/users", QueryParams: map[string][]string{"id": {"2"}}, StatusCode: 200},
		{Method: "POST", Path: "/api/users", QueryParams: map[string][]string{"id": {"1"}}, StatusCode: 201},
	}

	out := Apply(logs, cfg)
	if len(out) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(out))
	}
	var merged *types.TrafficLog
	for i := range out {
		if out[i].Method == "GET" && out[i].Path == "/api/users" && len(out[i].QueryParams["id"]) == 1 && out[i].QueryParams["id"][0] == "1" {
			merged = &out[i]
		}
	}
	if merged == nil {
		t.Fatalf("expected merged GET /api/users?id=1")
	}
	if merged.CallCount != 2 {
		t.Fatalf("expected CallCount 2, got %d", merged.CallCount)
	}
}

func TestApplyRemoveConsecutive5xxRetries(t *testing.T) {
	cfg := FilterConfig{}
	logs := []types.TrafficLog{
		{Method: "GET", Path: "/api/retry", StatusCode: 500},
		{Method: "GET", Path: "/api/retry", StatusCode: 502},
		{Method: "GET", Path: "/api/retry", StatusCode: 503},
	}

	out := Apply(logs, cfg)
	if len(out) != 1 {
		t.Fatalf("expected 1 log after 5xx dedup, got %d", len(out))
	}
	if out[0].CallCount != 1 {
		t.Fatalf("expected CallCount 1 after dedup, got %d", out[0].CallCount)
	}
	if out[0].StatusCode != 500 {
		t.Fatalf("expected to keep first 5xx, got %d", out[0].StatusCode)
	}
}
