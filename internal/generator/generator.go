package generator

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yourorg/apidoc/internal/config"
	"github.com/yourorg/apidoc/internal/filter"
	"github.com/yourorg/apidoc/internal/store"
	"github.com/yourorg/apidoc/pkg/types"
)

// LLMConfig is an alias of config.LLMConfig.
type LLMConfig = config.LLMConfig

// ProgressFunc reports generation progress.
type ProgressFunc func(stage string)

// Generate orchestrates filtering, sanitization, batching, LLM calls, caching and rendering.
func Generate(sess *types.Session, logs []types.TrafficLog, llmCfg LLMConfig, st store.Store, onProgress ProgressFunc, noCache bool, resume bool) (*types.GeneratedDoc, error) {
	if sess == nil {
		return nil, errors.New("session is nil")
	}
	if st == nil {
		return nil, errors.New("store is nil")
	}

	cfg := &config.Config{}
	cfg.SetDefaults()

	report(onProgress, "filtering logs")
	filtered := filter.Apply(logs, cfg.Filter)
	report(onProgress, "sanitizing logs")
	sanitized := filter.Sanitize(filtered, cfg.Sanitize)

	if err := st.UpdateSessionStatus(sess.ID, "generating"); err != nil {
		return nil, err
	}

	if noCache {
		if err := st.ClearCaches(sess.ID); err != nil {
			return nil, err
		}
	}

	batches := [][]types.TrafficLog{sanitized}
	if ShouldBatch(sanitized, llmCfg.MaxTokens) {
		report(onProgress, "splitting batches")
		batches = SplitBatches(sanitized, llmCfg.MaxTokens)
	}

	var caches []types.LLMCache
	if resume {
		var err error
		caches, err = st.GetBatchCaches(sess.ID)
		if err != nil {
			return nil, err
		}
	}

	var allDocs []*types.GeneratedDoc
	hasFailure := false
	for i, batch := range batches {
		if resume {
			if cache, ok := cacheByIndex(caches, i); ok && cache.Status == "ok" {
				report(onProgress, fmt.Sprintf("batch %d/%d: using cache", i+1, len(batches)))
				doc, err := parseCachedDoc(cache)
				if err == nil {
					allDocs = append(allDocs, doc)
					continue
				}
			}
		}

		report(onProgress, fmt.Sprintf("batch %d/%d: calling LLM", i+1, len(batches)))
		doc, raw, err := callLLM(sess, batch, llmCfg)
		cache := &types.LLMCache{
			SessionID:  sess.ID,
			BatchIndex: i,
			BatchKey:   batchKey(batch),
			Model:      llmCfg.Model,
		}
		if err != nil {
			cache.Status = "failed"
			cache.ErrorMsg = err.Error()
			hasFailure = true
		} else {
			cache.Status = "ok"
			cache.RawOutput = raw
			allDocs = append(allDocs, doc)
		}
		if err := st.SaveBatchCache(cache); err != nil {
			return nil, err
		}
	}

	if len(allDocs) == 0 {
		_ = st.UpdateSessionStatus(sess.ID, "failed")
		return nil, errors.New("all batches failed")
	}

	merged := MergeDocs(allDocs)

	report(onProgress, "rendering outputs")
	for _, format := range cfg.Output.Formats {
		switch format {
		case "markdown":
			if err := RenderMarkdown(merged, cfg.Output.Dir); err != nil {
				return nil, err
			}
		case "openapi":
			if err := RenderOpenAPI(merged, cfg.Output.Dir); err != nil {
				return nil, err
			}
		}
	}

	if hasFailure {
		if err := st.UpdateSessionStatus(sess.ID, "partial_generated"); err != nil {
			return nil, err
		}
	} else {
		if err := st.UpdateSessionStatus(sess.ID, "generated"); err != nil {
			return nil, err
		}
	}

	return merged, nil
}

func report(fn ProgressFunc, msg string) {
	if fn != nil {
		fn(msg)
	}
}

func cacheByIndex(caches []types.LLMCache, idx int) (types.LLMCache, bool) {
	for _, c := range caches {
		if c.BatchIndex == idx {
			return c, true
		}
	}
	return types.LLMCache{}, false
}

func parseCachedDoc(cache types.LLMCache) (*types.GeneratedDoc, error) {
	if cache.RawOutput == "" {
		return nil, errors.New("empty cache output")
	}
	var doc types.GeneratedDoc
	if err := json.Unmarshal([]byte(cache.RawOutput), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func callLLM(sess *types.Session, batch []types.TrafficLog, cfg LLMConfig) (*types.GeneratedDoc, string, error) {
	client := &Client{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
	}
	system := BuildSystemPrompt()
	user := BuildUserPrompt(sess.Scenario, batch)
	content, err := client.Chat(system, user)
	if err != nil {
		return nil, "", err
	}
	content = stripMarkdownCodeBlock(content)
	var doc types.GeneratedDoc
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return nil, "", err
	}
	if doc.Scenario == "" {
		doc.Scenario = sess.Scenario
	}
	return &doc, content, nil
}

func batchKey(batch []types.TrafficLog) string {
	if len(batch) == 0 {
		return "empty"
	}
	keys := make(map[string]struct{})
	for _, l := range batch {
		keys[pathPrefix(l.Path)] = struct{}{}
	}
	if len(keys) == 1 {
		for k := range keys {
			return k
		}
	}
	return "mixed"
}
