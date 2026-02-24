package generator

import (
	"strings"
	"testing"

	"github.com/yourorg/apidoc/pkg/types"
)

func TestBuildSystemPromptContainsRules(t *testing.T) {
	sys := BuildSystemPrompt()
	if !strings.Contains(sys, "只输出 JSON") {
		t.Fatalf("expected system prompt to emphasize JSON-only output")
	}
	if !strings.Contains(sys, "类型推断规则") {
		t.Fatalf("expected system prompt to include type inference rules")
	}
	if !strings.Contains(sys, "中文") {
		t.Fatalf("expected system prompt to mention Chinese descriptions")
	}
}

func TestBuildUserPromptIncludesScenarioAndExample(t *testing.T) {
	logs := []types.TrafficLog{{Method: "GET", Path: "/api/test"}}
	prompt := BuildUserPrompt("login flow", logs)
	if !strings.Contains(prompt, "login flow") {
		t.Fatalf("expected prompt to include scenario")
	}
	if !strings.Contains(prompt, "输出示例") {
		t.Fatalf("expected prompt to include few-shot example")
	}
}

func TestBuildUserPromptBodyTruncation(t *testing.T) {
	long := strings.Repeat("x", 2100)
	body := `{"a":{"b":"` + long + `"},"c":1}`
	logs := []types.TrafficLog{{Method: "POST", Path: "/api/large", RequestBody: body}}
	prompt := BuildUserPrompt("big body", logs)
	if strings.Contains(prompt, long) {
		t.Fatalf("expected long nested body to be truncated")
	}
	if !strings.Contains(prompt, "[truncated]") {
		t.Fatalf("expected truncation placeholder")
	}
}

func TestBuildUserPromptDedupByPath(t *testing.T) {
	logs := make([]types.TrafficLog, 0, 31)
	for i := 0; i < 31; i++ {
		logs = append(logs, types.TrafficLog{Method: "GET", Path: "/api/dup"})
	}
	prompt := BuildUserPrompt("dedup", logs)
	if strings.Count(prompt, "/api/dup") != 1 {
		t.Fatalf("expected path to appear once after dedup")
	}
}

func TestBuildUserPromptCallCountNote(t *testing.T) {
	logs := []types.TrafficLog{{Method: "GET", Path: "/api/call", CallCount: 3}}
	prompt := BuildUserPrompt("note", logs)
	if !strings.Contains(prompt, "此 API 被调用了 3 次") {
		t.Fatalf("expected call count note")
	}
}
