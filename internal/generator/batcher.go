package generator

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/yourorg/apidoc/pkg/types"
)

// EstimateTokens provides a rough token estimate.
// Chinese is ~2 chars/token, others ~4 chars/token.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var chinese, other int
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			chinese++
			continue
		}
		other++
	}
	return (chinese+1)/2 + (other+3)/4
}

// ShouldBatch determines whether logs likely exceed the token limit.
func ShouldBatch(logs []types.TrafficLog, maxTokens int) bool {
	if maxTokens <= 0 {
		return false
	}
	b, _ := json.Marshal(logs)
	return EstimateTokens(string(b)) > maxTokens
}

// SplitBatches groups logs by path prefix and splits into batches.
func SplitBatches(logs []types.TrafficLog, maxTokens int) [][]types.TrafficLog {
	if len(logs) == 0 {
		return nil
	}
	if maxTokens <= 0 {
		return [][]types.TrafficLog{logs}
	}
	groups := make(map[string][]types.TrafficLog)
	order := make([]string, 0)
	for _, l := range logs {
		key := pathPrefix(l.Path)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], l)
	}

	var batches [][]types.TrafficLog
	var current []types.TrafficLog
	currentTokens := 0
	for _, key := range order {
		group := groups[key]
		groupTokens := estimateTokensForLogs(group)
		if len(current) == 0 {
			if groupTokens > maxTokens {
				batches = append(batches, group)
				continue
			}
			current = append(current, group...)
			currentTokens = groupTokens
			continue
		}
		if currentTokens+groupTokens > maxTokens {
			batches = append(batches, current)
			if groupTokens > maxTokens {
				batches = append(batches, group)
				current = nil
				currentTokens = 0
				continue
			}
			current = append([]types.TrafficLog{}, group...)
			currentTokens = groupTokens
			continue
		}
		current = append(current, group...)
		currentTokens += groupTokens
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// MergeDocs merges docs from multiple batches, de-duplicating endpoints by method+path.
func MergeDocs(docs []*types.GeneratedDoc) *types.GeneratedDoc {
	merged := &types.GeneratedDoc{}
	seen := make(map[string]struct{})
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		if merged.Scenario == "" {
			merged.Scenario = doc.Scenario
		}
		for _, ep := range doc.Endpoints {
			key := strings.ToUpper(ep.Method) + " " + ep.Path
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged.Endpoints = append(merged.Endpoints, ep)
		}
		merged.CallChain = doc.CallChain
	}
	return merged
}

func estimateTokensForLogs(logs []types.TrafficLog) int {
	b, _ := json.Marshal(logs)
	return EstimateTokens(string(b))
}

func pathPrefix(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "/"
	}
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return "/" + strings.Join(parts, "/")
}
