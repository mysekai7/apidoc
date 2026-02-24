package generator

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/yourorg/apidoc/pkg/types"
)

func TestRenderMarkdown(t *testing.T) {
	doc := &types.GeneratedDoc{
		Scenario: "Test Scenario",
		CallChain: []types.ChainStep{{Seq: 1, Method: "GET", Path: "/users", Description: "list users"}},
		Endpoints: []types.Endpoint{{
			Method:      "GET",
			Path:        "/users",
			Summary:     "List users",
			Description: "Returns users",
			QueryParams: []types.Param{{Name: "page", Type: "integer", Required: false, Description: "page"}},
			Responses:   []types.Response{{StatusCode: 200, ContentType: "application/json", Description: "ok"}},
		}},
	}
	outDir := t.TempDir()
	if err := RenderMarkdown(doc, outDir); err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(outDir, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if string(readme) == "" {
		t.Fatalf("README should not be empty")
	}
	apiDocs, err := os.ReadFile(filepath.Join(outDir, "api-docs.md"))
	if err != nil {
		t.Fatalf("read api-docs: %v", err)
	}
	if string(apiDocs) == "" {
		t.Fatalf("api-docs should not be empty")
	}
}

func TestRenderOpenAPIAndValidate(t *testing.T) {
	doc := &types.GeneratedDoc{
		Scenario: "Test",
		Endpoints: []types.Endpoint{{
			Method:      "POST",
			Path:        "/users",
			Summary:     "Create user",
			Tags:        []string{"Users"},
			Description: "Creates a user",
			RequestBody: &types.BodySchema{
				ContentType: "application/json",
				Fields: []types.Param{{
					Name:     "user",
					Type:     "object",
					Required: true,
					Children: []types.Param{{Name: "id", Type: "string (uuid)", Required: true}, {Name: "name", Type: "string", Required: true}},
				}},
			},
			Responses: []types.Response{{
				StatusCode:  200,
				ContentType: "application/json",
				Description: "ok",
				Fields:      []types.Param{{Name: "id", Type: "string (uuid)", Required: true}},
			}},
		}},
	}
	outDir := t.TempDir()
	if err := RenderOpenAPI(doc, outDir); err != nil {
		t.Fatalf("RenderOpenAPI error: %v", err)
	}
	path := filepath.Join(outDir, "openapi.yaml")
	if errs := ValidateOpenAPI(path); len(errs) != 0 {
		t.Fatalf("expected no validation errors, got %v", errs)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var spec map[string]interface{}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	paths := spec["paths"].(map[string]interface{})
	post := paths["/users"].(map[string]interface{})["post"].(map[string]interface{})
	rb := post["requestBody"].(map[string]interface{})
	content := rb["content"].(map[string]interface{})
	appJSON := content["application/json"].(map[string]interface{})
	schema := appJSON["schema"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	user := props["user"].(map[string]interface{})
	userProps := user["properties"].(map[string]interface{})
	if _, ok := userProps["id"]; !ok {
		t.Fatalf("expected nested user.id schema")
	}
}
