package generator

import (
	"encoding/json"
	"fmt"

	"github.com/yourorg/apidoc/pkg/types"
)

const systemPrompt = `你是一个 API 文档专家。你会收到：
1. 用户对操作场景的描述
2. 一组按时间排序的 HTTP 请求/响应记录（来自真实流量采集）

你的任务：
1. 分析调用链路，理解每个 API 的作用和调用顺序
2. 为每个独立的 API 端点生成文档：
   - 路径、方法、功能描述、Tags 分组
   - Path/Query/Body 参数说明（名称、类型、是否必填、含义）
   - 响应字段说明（支持嵌套结构）
   - 示例请求和响应（基于真实数据脱敏后）
3. 生成场景调用链路图（哪个 API 先调、为什么、数据如何流转）
4. 如果同一 API 被调用多次（不同参数），合并为一个端点，列出所有参数组合
5. 输出严格按照指定 JSON schema，只输出 JSON，不要包裹在 markdown 代码块中

类型推断规则：
- UUID 格式字符串 → string (uuid)
- ISO 8601 时间 → string (datetime)
- 纯整数 → integer
- 带小数 → number
- true/false → boolean
- 数组 → array，标注元素类型

请用中文撰写所有描述性文字，字段名保持英文原样。
只输出 JSON，不要 markdown 代码块。`

const userPromptExample = `## 输出示例（仅供参考格式）
{
  "scenario": "查看用户列表",
  "call_chain": [
    {"seq": 1, "method": "GET", "path": "/api/v1/users", "description": "获取用户列表", "depends_on": null}
  ],
  "endpoints": [
    {
      "method": "GET",
      "path": "/api/v1/users",
      "summary": "获取用户列表",
      "tags": ["用户管理"],
      "description": "分页查询系统中的用户列表",
      "query_params": [{"name": "page", "type": "integer", "required": false, "description": "页码"}],
      "responses": [
        {
          "status_code": 200,
          "content_type": "application/json",
          "description": "成功返回用户列表",
          "fields": [
            {"name": "total", "type": "integer", "required": true, "description": "总数"},
            {"name": "items", "type": "array", "required": true, "description": "用户数组", "children": [
              {"name": "id", "type": "string (uuid)", "required": true, "description": "用户ID"}
            ]}
          ]
        }
      ]
    }
  ]
}

请分析以上流量，生成该场景的完整 API 文档。`

// BuildSystemPrompt returns the static system prompt.
func BuildSystemPrompt() string {
	return systemPrompt
}

// BuildUserPrompt builds a user prompt with scenario and traffic records.
func BuildUserPrompt(scenario string, logs []types.TrafficLog) string {
	filtered := logs
	if len(filtered) > 30 {
		seen := make(map[string]struct{})
		dedup := make([]types.TrafficLog, 0, len(filtered))
		for _, l := range filtered {
			if _, ok := seen[l.Path]; ok {
				continue
			}
			seen[l.Path] = struct{}{}
			dedup = append(dedup, l)
		}
		filtered = dedup
	}

	records := make([]map[string]interface{}, 0, len(filtered))
	for _, l := range filtered {
		body := l.RequestBody
		if len(body) > 2000 {
			if truncated, ok := truncateJSONBody(body); ok {
				body = truncated
			}
		}
		rec := map[string]interface{}{
			"seq":                   l.Seq,
			"method":                l.Method,
			"path":                  l.Path,
			"query_params":          l.QueryParams,
			"request_headers":       l.RequestHeaders,
			"request_body":          body,
			"content_type":          l.ContentType,
			"status_code":           l.StatusCode,
			"response_headers":      l.ResponseHeaders,
			"response_body":          l.ResponseBody,
			"response_content_type": l.ResponseContentType,
			"call_count":            l.CallCount,
		}
		if l.CallCount > 1 {
			rec["note"] = fmt.Sprintf("此 API 被调用了 %d 次", l.CallCount)
		}
		records = append(records, rec)
	}

	b, _ := json.MarshalIndent(records, "", "  ")
	return fmt.Sprintf("## 场景描述\n%s\n\n## API 调用记录（共 %d 条，按时间排序）\n%s\n\n%s", scenario, len(records), string(b), userPromptExample)
}

func truncateJSONBody(body string) (string, bool) {
	var v interface{}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return "", false
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return "", false
	}
	truncated := make(map[string]interface{}, len(m))
	for k, val := range m {
		switch val.(type) {
		case map[string]interface{}, []interface{}:
			truncated[k] = "[truncated]"
		default:
			if s, ok := val.(string); ok && len(s) > 200 {
				truncated[k] = "[truncated]"
			} else {
				truncated[k] = val
			}
		}
	}
	b, err := json.Marshal(truncated)
	if err != nil {
		return "", false
	}
	return string(b), true
}
