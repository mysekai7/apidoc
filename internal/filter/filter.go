package filter

import (
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/yourorg/apidoc/internal/config"
	"github.com/yourorg/apidoc/pkg/types"
)

// FilterConfig is an alias of config.FilterConfig.
type FilterConfig = config.FilterConfig

// Apply filters and merges traffic logs based on config rules.
func Apply(logs []types.TrafficLog, cfg FilterConfig) []types.TrafficLog {
	filtered := make([]types.TrafficLog, 0, len(logs))
	for _, l := range logs {
		if strings.EqualFold(l.Method, "OPTIONS") {
			continue
		}
		if hasIgnoredExtension(l.Path, cfg.IgnoreExtensions) {
			continue
		}
		if matchesContentType(l.ResponseContentType, cfg.IgnoreContentTypes) {
			continue
		}
		if hasIgnoredPath(l.Path, cfg.IgnorePaths) {
			continue
		}
		filtered = append(filtered, l)
	}

	filtered = removeConsecutive5xx(filtered)
	return mergeIdentical(filtered)
}

func hasIgnoredExtension(p string, exts []string) bool {
	ext := strings.ToLower(path.Ext(p))
	if ext == "" {
		return false
	}
	for _, e := range exts {
		if strings.ToLower(strings.TrimSpace(e)) == ext {
			return true
		}
	}
	return false
}

func hasIgnoredPath(p string, prefixes []string) bool {
	for _, pref := range prefixes {
		pref = strings.TrimSpace(pref)
		if pref == "" {
			continue
		}
		if strings.HasPrefix(p, pref) {
			return true
		}
	}
	return false
}

func matchesContentType(ct string, ignores []string) bool {
	if strings.TrimSpace(ct) == "" {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	for _, p := range ignores {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "/*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(base, prefix) {
				return true
			}
			continue
		}
		if base == p {
			return true
		}
	}
	return false
}

func removeConsecutive5xx(logs []types.TrafficLog) []types.TrafficLog {
	out := make([]types.TrafficLog, 0, len(logs))
	var prevKey string
	var prevWas5xx bool
	for _, l := range logs {
		key := requestKey(l.Method, l.Path, l.QueryParams)
		if prevWas5xx && key == prevKey && is5xx(l.StatusCode) {
			continue
		}
		out = append(out, l)
		prevKey = key
		prevWas5xx = is5xx(l.StatusCode)
	}
	return out
}

func mergeIdentical(logs []types.TrafficLog) []types.TrafficLog {
	out := make([]types.TrafficLog, 0, len(logs))
	index := make(map[string]int, len(logs))
	for _, l := range logs {
		key := requestKey(l.Method, l.Path, l.QueryParams)
		if idx, ok := index[key]; ok {
			count := l.CallCount
			if count == 0 {
				count = 1
			}
			out[idx].CallCount += count
			continue
		}
		if l.CallCount == 0 {
			l.CallCount = 1
		}
		index[key] = len(out)
		out = append(out, l)
	}
	return out
}

func is5xx(code int) bool {
	return code >= 500 && code <= 599
}

func requestKey(method, path string, params map[string][]string) string {
	return strings.ToUpper(method) + " " + path + "?" + canonicalQuery(params)
}

func canonicalQuery(params map[string][]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vals := url.Values{}
	for _, k := range keys {
		values := append([]string(nil), params[k]...)
		sort.Strings(values)
		for _, v := range values {
			vals.Add(k, v)
		}
	}
	return vals.Encode()
}
