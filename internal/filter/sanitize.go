package filter

import (
	"encoding/json"
	"strings"

	"github.com/yourorg/apidoc/internal/config"
	"github.com/yourorg/apidoc/pkg/types"
)

// SanitizeConfig is an alias of config.SanitizeConfig.
type SanitizeConfig = config.SanitizeConfig

// Sanitize redacts sensitive data in headers, query params and JSON bodies.
func Sanitize(logs []types.TrafficLog, cfg SanitizeConfig) []types.TrafficLog {
	headerSet := toLowerSet(cfg.Headers)
	fieldSet := toLowerSet(cfg.BodyFields)
	replacement := cfg.Replacement
	out := make([]types.TrafficLog, len(logs))
	for i, l := range logs {
		out[i] = l
		out[i].RequestHeaders = sanitizeHeaderMap(l.RequestHeaders, headerSet, replacement)
		out[i].ResponseHeaders = sanitizeHeaderMap(l.ResponseHeaders, headerSet, replacement)
		out[i].QueryParams = sanitizeQueryParams(l.QueryParams, fieldSet, replacement)
		out[i].RequestBody = sanitizeBody(l.RequestBody, fieldSet, replacement)
	}
	return out
}

func toLowerSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, v := range items {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	return set
}

func sanitizeHeaderMap(in map[string]string, set map[string]struct{}, replacement string) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if _, ok := set[strings.ToLower(k)]; ok {
			out[k] = replacement
			continue
		}
		out[k] = v
	}
	return out
}

func sanitizeQueryParams(in map[string][]string, set map[string]struct{}, replacement string) map[string][]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string][]string, len(in))
	for k, vs := range in {
		if _, ok := set[strings.ToLower(k)]; ok {
			repl := make([]string, len(vs))
			for i := range repl {
				repl[i] = replacement
			}
			out[k] = repl
			continue
		}
		cpy := append([]string(nil), vs...)
		out[k] = cpy
	}
	return out
}

func sanitizeBody(body string, set map[string]struct{}, replacement string) string {
	if strings.TrimSpace(body) == "" {
		return body
	}
	var v interface{}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return body
	}
	v = sanitizeJSONValue(v, set, replacement)
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return string(out)
}

func sanitizeJSONValue(v interface{}, set map[string]struct{}, replacement string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, v2 := range val {
			if _, ok := set[strings.ToLower(k)]; ok {
				val[k] = replacement
				continue
			}
			val[k] = sanitizeJSONValue(v2, set, replacement)
		}
		return val
	case []interface{}:
		for i := range val {
			val[i] = sanitizeJSONValue(val[i], set, replacement)
		}
		return val
	default:
		return val
	}
}
