package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yourorg/apidoc/pkg/types"
)

// RenderMarkdown renders README.md and api-docs.md.
func RenderMarkdown(doc *types.GeneratedDoc, outputDir string) error {
	if doc == nil {
		return fmt.Errorf("doc is nil")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	readme := &strings.Builder{}
	fmt.Fprintf(readme, "# %s\n\n", doc.Scenario)
	fmt.Fprintln(readme, "## Call Chain")
	for _, step := range doc.CallChain {
		if step.DependsOn != nil {
			fmt.Fprintf(readme, "- %d. %s %s — %s (depends on %d)\n", step.Seq, step.Method, step.Path, step.Description, *step.DependsOn)
		} else {
			fmt.Fprintf(readme, "- %d. %s %s — %s\n", step.Seq, step.Method, step.Path, step.Description)
		}
	}

	apiDocs := &strings.Builder{}
	fmt.Fprintln(apiDocs, "# API Docs")
	for _, ep := range doc.Endpoints {
		fmt.Fprintf(apiDocs, "\n## %s %s\n", ep.Method, ep.Path)
		if ep.Summary != "" {
			fmt.Fprintf(apiDocs, "**Summary:** %s\n\n", ep.Summary)
		}
		if ep.Description != "" {
			fmt.Fprintf(apiDocs, "**Description:** %s\n\n", ep.Description)
		}
		if len(ep.Tags) > 0 {
			fmt.Fprintf(apiDocs, "**Tags:** %s\n\n", strings.Join(ep.Tags, ", "))
		}
		if len(ep.PathParams) > 0 {
			fmt.Fprintln(apiDocs, "### Path Parameters")
			apiDocs.WriteString(renderParams(ep.PathParams, ""))
			apiDocs.WriteString("\n")
		}
		if len(ep.QueryParams) > 0 {
			fmt.Fprintln(apiDocs, "### Query Parameters")
			apiDocs.WriteString(renderParams(ep.QueryParams, ""))
			apiDocs.WriteString("\n")
		}
		if ep.RequestBody != nil {
			fmt.Fprintf(apiDocs, "### Request Body (%s)\n", ep.RequestBody.ContentType)
			apiDocs.WriteString(renderParams(ep.RequestBody.Fields, ""))
			apiDocs.WriteString("\n")
		}
		if len(ep.Responses) > 0 {
			fmt.Fprintln(apiDocs, "### Responses")
			for _, resp := range ep.Responses {
				fmt.Fprintf(apiDocs, "- %d (%s): %s\n", resp.StatusCode, resp.ContentType, resp.Description)
				if len(resp.Fields) > 0 {
					apiDocs.WriteString(renderParams(resp.Fields, "  "))
				}
			}
			apiDocs.WriteString("\n")
		}
	}

	if err := os.WriteFile(filepath.Join(outputDir, "README.md"), []byte(readme.String()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "api-docs.md"), []byte(apiDocs.String()), 0o644); err != nil {
		return err
	}
	return nil
}

func renderParams(params []types.Param, indent string) string {
	b := &strings.Builder{}
	for _, p := range params {
		req := "optional"
		if p.Required {
			req = "required"
		}
		fmt.Fprintf(b, "%s- %s (%s, %s): %s\n", indent, p.Name, p.Type, req, p.Description)
		if len(p.Children) > 0 {
			b.WriteString(renderParams(p.Children, indent+"  "))
		}
	}
	return b.String()
}

// RenderOpenAPI renders OpenAPI 3.0 YAML to outputDir/openapi.yaml.
func RenderOpenAPI(doc *types.GeneratedDoc, outputDir string) error {
	if doc == nil {
		return fmt.Errorf("doc is nil")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":   doc.Scenario,
			"version": "1.0.0",
		},
		"paths": map[string]interface{}{},
	}

	// tags
	tagSet := make(map[string]struct{})
	for _, ep := range doc.Endpoints {
		for _, tag := range ep.Tags {
			tagSet[tag] = struct{}{}
		}
	}
	if len(tagSet) > 0 {
		tags := make([]map[string]interface{}, 0, len(tagSet))
		names := make([]string, 0, len(tagSet))
		for name := range tagSet {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			tags = append(tags, map[string]interface{}{"name": name})
		}
		spec["tags"] = tags
	}

	paths := spec["paths"].(map[string]interface{})
	for _, ep := range doc.Endpoints {
		method := strings.ToLower(ep.Method)
		pathItem, ok := paths[ep.Path].(map[string]interface{})
		if !ok {
			pathItem = map[string]interface{}{}
			paths[ep.Path] = pathItem
		}
		op := map[string]interface{}{}
		if ep.Summary != "" {
			op["summary"] = ep.Summary
		}
		if ep.Description != "" {
			op["description"] = ep.Description
		}
		if len(ep.Tags) > 0 {
			op["tags"] = ep.Tags
		}

		params := make([]map[string]interface{}, 0)
		for _, p := range ep.PathParams {
			params = append(params, paramToOpenAPIParam(p, "path"))
		}
		for _, p := range ep.QueryParams {
			params = append(params, paramToOpenAPIParam(p, "query"))
		}
		if len(params) > 0 {
			op["parameters"] = params
		}

		if ep.RequestBody != nil {
			schema := buildObjectSchema(ep.RequestBody.Fields)
			if ep.Example != nil && ep.Example.Request != "" {
				schema["example"] = ep.Example.Request
			}
			op["requestBody"] = map[string]interface{}{
				"content": map[string]interface{}{
					ep.RequestBody.ContentType: map[string]interface{}{
						"schema": schema,
					},
				},
			}
		}

		responses := map[string]interface{}{}
		for _, resp := range ep.Responses {
			respObj := map[string]interface{}{"description": resp.Description}
			if resp.ContentType != "" {
				schema := buildObjectSchema(resp.Fields)
				if ep.Example != nil && ep.Example.Response != "" {
					schema["example"] = ep.Example.Response
				}
				respObj["content"] = map[string]interface{}{
					resp.ContentType: map[string]interface{}{
						"schema": schema,
					},
				}
			}
			responses[fmt.Sprintf("%d", resp.StatusCode)] = respObj
		}
		op["responses"] = responses

		pathItem[method] = op
	}

	data, err := yaml.Marshal(spec)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "openapi.yaml"), data, 0o644)
}

func paramToOpenAPIParam(p types.Param, in string) map[string]interface{} {
	schema := paramToSchema(p)
	return map[string]interface{}{
		"name":        p.Name,
		"in":          in,
		"required":    p.Required,
		"description": p.Description,
		"schema":      schema,
	}
}

func paramToSchema(p types.Param) map[string]interface{} {
	typeName, format := inferType(p.Type)
	schema := map[string]interface{}{}
	if typeName != "" {
		schema["type"] = typeName
	}
	if format != "" {
		schema["format"] = format
	}
	if p.Description != "" {
		schema["description"] = p.Description
	}
	if len(p.Children) > 0 {
		if typeName == "array" {
			schema["items"] = buildObjectSchema(p.Children)
		} else {
			child := buildObjectSchema(p.Children)
			schema["type"] = "object"
			for k, v := range child {
				schema[k] = v
			}
		}
	}
	return schema
}

func buildObjectSchema(fields []types.Param) map[string]interface{} {
	props := map[string]interface{}{}
	var required []string
	for _, f := range fields {
		props[f.Name] = paramToSchema(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	schema := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func inferType(t string) (string, string) {
	lt := strings.ToLower(t)
	switch {
	case strings.Contains(lt, "uuid"):
		return "string", "uuid"
	case strings.Contains(lt, "datetime"):
		return "string", "date-time"
	case strings.Contains(lt, "integer"):
		return "integer", ""
	case strings.Contains(lt, "number"):
		return "number", ""
	case strings.Contains(lt, "boolean"):
		return "boolean", ""
	case strings.Contains(lt, "array"):
		return "array", ""
	case strings.Contains(lt, "object"):
		return "object", ""
	case strings.Contains(lt, "string"):
		return "string", ""
	default:
		return "string", ""
	}
}

// ValidateOpenAPI performs basic validation for generated OpenAPI YAML.
func ValidateOpenAPI(yamlPath string) []string {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return []string{err.Error()}
	}
	var spec map[string]interface{}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return []string{err.Error()}
	}
	var errs []string
	if _, ok := spec["openapi"]; !ok {
		errs = append(errs, "missing openapi field")
	}
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok || len(paths) == 0 {
		errs = append(errs, "missing or empty paths")
		return errs
	}
	for p, v := range paths {
		if _, ok := v.(map[string]interface{}); !ok {
			errs = append(errs, fmt.Sprintf("invalid path item for %s", p))
		}
	}
	return errs
}
