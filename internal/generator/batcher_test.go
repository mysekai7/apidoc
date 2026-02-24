package generator

import (
	"strings"
	"testing"

	"github.com/yourorg/apidoc/pkg/types"
)

func TestEstimateTokens(t *testing.T) {
	text := "你好hello"
	got := EstimateTokens(text)
	if got != 3 {
		t.Fatalf("expected 3 tokens, got %d", got)
	}
}

func TestShouldBatch(t *testing.T) {
	logs := []types.TrafficLog{{RequestBody: strings.Repeat("a", 200)}}
	if !ShouldBatch(logs, 10) {
		t.Fatalf("expected ShouldBatch to be true")
	}
}

func TestSplitBatchesByPrefix(t *testing.T) {
	logs := []types.TrafficLog{
		{Method: "GET", Path: "/api/v1/users/1"},
		{Method: "GET", Path: "/api/v1/users/2"},
		{Method: "GET", Path: "/api/v1/projects/1"},
		{Method: "POST", Path: "/auth/login"},
	}
	batches := SplitBatches(logs, 1)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(batches))
	}
	if batches[0][0].Path != "/api/v1/users/1" {
		t.Fatalf("expected users batch first")
	}
}

func TestMergeDocs(t *testing.T) {
	doc1 := &types.GeneratedDoc{Scenario: "s", CallChain: []types.ChainStep{{Seq: 1}}, Endpoints: []types.Endpoint{{Method: "GET", Path: "/a"}}}
	doc2 := &types.GeneratedDoc{Scenario: "s", CallChain: []types.ChainStep{{Seq: 1}, {Seq: 2}}, Endpoints: []types.Endpoint{{Method: "GET", Path: "/a"}, {Method: "POST", Path: "/b"}}}

	merged := MergeDocs([]*types.GeneratedDoc{doc1, doc2})
	if len(merged.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(merged.Endpoints))
	}
	if len(merged.CallChain) != 2 {
		t.Fatalf("expected call chain from last doc")
	}
}
