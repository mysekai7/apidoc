package filter

import (
	"encoding/json"
	"testing"

	"github.com/yourorg/apidoc/pkg/types"
)

func TestSanitizeHeadersAndQuery(t *testing.T) {
	cfg := SanitizeConfig{
		Headers:     []string{"Authorization", "X-Api-Key", "Set-Cookie"},
		BodyFields:  []string{"token", "password"},
		Replacement: "***REDACTED***",
	}
	logs := []types.TrafficLog{
		{
			RequestHeaders:  map[string]string{"authorization": "Bearer abc", "X-API-Key": "k", "Accept": "application/json"},
			ResponseHeaders: map[string]string{"Set-Cookie": "secret=1", "Content-Type": "application/json"},
			QueryParams:     map[string][]string{"token": {"abc", "def"}, "q": {"ok"}},
		},
	}

	out := Sanitize(logs, cfg)
	got := out[0]
	if got.RequestHeaders["authorization"] != cfg.Replacement {
		t.Fatalf("expected authorization redacted")
	}
	if got.RequestHeaders["X-API-Key"] != cfg.Replacement {
		t.Fatalf("expected x-api-key redacted")
	}
	if got.RequestHeaders["Accept"] != "application/json" {
		t.Fatalf("expected accept unchanged")
	}
	if got.ResponseHeaders["Set-Cookie"] != cfg.Replacement {
		t.Fatalf("expected set-cookie redacted")
	}
	if got.QueryParams["token"][0] != cfg.Replacement || got.QueryParams["token"][1] != cfg.Replacement {
		t.Fatalf("expected token query params redacted")
	}
	if got.QueryParams["q"][0] != "ok" {
		t.Fatalf("expected non-sensitive query param unchanged")
	}
}

func TestSanitizeBodyNested(t *testing.T) {
	cfg := SanitizeConfig{
		BodyFields:  []string{"password", "token", "secret"},
		Replacement: "***REDACTED***",
	}
	body := `{"user":{"password":"p","profile":{"token":"t","age":30}},"items":[{"secret":"s1"},{"name":"n"}],"token":"top"}`
	logs := []types.TrafficLog{{RequestBody: body}}

	out := Sanitize(logs, cfg)
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out[0].RequestBody), &got); err != nil {
		t.Fatalf("unexpected json error: %v", err)
	}
	user := got["user"].(map[string]interface{})
	if user["password"] != cfg.Replacement {
		t.Fatalf("expected nested password redacted")
	}
	profile := user["profile"].(map[string]interface{})
	if profile["token"] != cfg.Replacement {
		t.Fatalf("expected nested token redacted")
	}
	items := got["items"].([]interface{})
	item0 := items[0].(map[string]interface{})
	if item0["secret"] != cfg.Replacement {
		t.Fatalf("expected secret redacted")
	}
	if got["token"] != cfg.Replacement {
		t.Fatalf("expected top-level token redacted")
	}
}

func TestSanitizeNonJSONBody(t *testing.T) {
	cfg := SanitizeConfig{BodyFields: []string{"password"}, Replacement: "***REDACTED***"}
	logs := []types.TrafficLog{{RequestBody: "not-json"}}

	out := Sanitize(logs, cfg)
	if out[0].RequestBody != "not-json" {
		t.Fatalf("expected non-json body unchanged")
	}
}
