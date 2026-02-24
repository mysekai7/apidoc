# API Doc Assistant — 详细开发计划 v3.1

## 概述

基于架构设计 v3.1，按 milestone 拆解为可执行的开发任务。每个任务包含具体实现要求和验收标准。

CLI 工具名：`apidoc`
语言：Go 1.22+
模块划分：config → har → store → filter → generator → server → extension

---

## 配置文件

`~/.apidoc/config.yaml`

```yaml
llm:
  provider: "openai"              # openai | azure | custom
  api_key: ""
  base_url: "https://api.openai.com/v1"
  model: "gpt-4o"
  max_tokens: 4096
  temperature: 0.2

output:
  dir: "./output"
  formats:
    - markdown
    - openapi

filter:
  ignore_extensions:
    - .js
    - .css
    - .png
    - .jpg
    - .gif
    - .svg
    - .woff
    - .woff2
    - .ico
    - .map
  ignore_content_types:
    - text/html
    - text/css
    - image/*
    - font/*
    - application/javascript
  ignore_paths:
    - /static/
    - /assets/
    - /favicon

sanitize:
  headers:
    - Authorization
    - Cookie
    - Set-Cookie
    - X-Api-Key
    - X-Auth-Token
  body_fields:
    - password
    - secret
    - token
    - api_key
    - access_token
    - refresh_token
    - credential
  replacement: "***REDACTED***"

server:
  host: "127.0.0.1"
  port: 3000
  cors_extension_id: ""           # Chrome 插件 ID（安装后从 chrome://extensions 获取）

log:
  level: "info"                   # debug | info | warn | error
```

---

## 公共数据结构

`pkg/types/types.go`

```go
package types

import "time"

// Session 录制会话
type Session struct {
    ID        string    `json:"id"`
    Source    string    `json:"source"`      // "har" | "extension"
    Scenario  string    `json:"scenario"`
    Host      string    `json:"host"`        // 目标服务 host
    LogCount  int       `json:"log_count"`   // 流量记录条数
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    Status    string    `json:"status"`      // "imported" | "generating" | "generated" | "partial_generated" | "failed"
}

// TrafficLog 单条 API 请求记录
type TrafficLog struct {
    ID                  int64               `json:"id"`
    SessionID           string              `json:"session_id"`
    Seq                 int                 `json:"seq"`
    Timestamp           time.Time           `json:"timestamp"`
    Method              string              `json:"method"`
    Host                string              `json:"host"`
    Path                string              `json:"path"`
    QueryParams         map[string][]string `json:"query_params,omitempty"`
    RequestHeaders      map[string]string   `json:"request_headers,omitempty"`
    RequestBody         string              `json:"request_body,omitempty"`
    RequestBodyEncoding string              `json:"request_body_encoding,omitempty"` // plain|base64|omitted
    ContentType         string              `json:"content_type,omitempty"`
    StatusCode          int                 `json:"status_code"`
    ResponseHeaders     map[string]string   `json:"response_headers,omitempty"`
    ResponseBody        string              `json:"response_body,omitempty"`
    ResponseContentType string              `json:"response_content_type,omitempty"`
    LatencyMs           int64               `json:"latency_ms"`
    CallCount           int                 `json:"call_count,omitempty"` // 合并后的调用次数
}

// GeneratedDoc LLM 输出的结构化文档
type GeneratedDoc struct {
    Scenario  string      `json:"scenario"`
    CallChain []ChainStep `json:"call_chain"`
    Endpoints []Endpoint  `json:"endpoints"`
}

// ChainStep 调用链中的一步
type ChainStep struct {
    Seq         int    `json:"seq"`
    Method      string `json:"method"`
    Path        string `json:"path"`
    Description string `json:"description"`
    DependsOn   *int   `json:"depends_on,omitempty"`
}

// Endpoint API 端点文档
type Endpoint struct {
    Method      string      `json:"method"`
    Path        string      `json:"path"`
    Summary     string      `json:"summary"`
    Tags        []string    `json:"tags,omitempty"`
    Description string      `json:"description"`
    PathParams  []Param     `json:"path_params,omitempty"`
    QueryParams []Param     `json:"query_params,omitempty"`
    RequestBody *BodySchema `json:"request_body,omitempty"`
    Responses   []Response  `json:"responses"`
    Example     *Example    `json:"example,omitempty"`
}

// Param 参数定义（支持嵌套）
type Param struct {
    Name        string  `json:"name"`
    Type        string  `json:"type"`
    Required    bool    `json:"required"`
    Description string  `json:"description"`
    Children    []Param `json:"children,omitempty"` // 嵌套子字段
}

// BodySchema 请求体结构
type BodySchema struct {
    ContentType string  `json:"content_type"`
    Fields      []Param `json:"fields"`
}

// Response 响应定义
type Response struct {
    StatusCode  int     `json:"status_code"`
    ContentType string  `json:"content_type,omitempty"`
    Description string  `json:"description"`
    Fields      []Param `json:"fields,omitempty"`
}

// Example 示例请求/响应
type Example struct {
    Request  string `json:"request"`
    Response string `json:"response"`
}

// LLMCache LLM 输出缓存（批次级）
type LLMCache struct {
    SessionID  string    `json:"session_id"`
    BatchIndex int       `json:"batch_index"`   // 批次序号（0-based）
    BatchKey   string    `json:"batch_key"`      // 批次标识（path 前缀分组名）
    Status     string    `json:"status"`         // "ok" | "failed"
    RawOutput  string    `json:"raw_output"`     // LLM 原始 JSON（失败时为空）
    Model      string    `json:"model"`
    TokensUsed int       `json:"tokens_used"`
    ErrorMsg   string    `json:"error_msg"`      // 失败时的错误信息
    CreatedAt  time.Time `json:"created_at"`
}
```

---

## 错误处理策略

### LLM 调用失败

| 场景 | 处理方式 |
|------|----------|
| 网络超时 / 5xx | 指数退避重试 3 次（1s→2s→4s），全部失败后 session 状态置为 `failed` |
| 429 Rate Limit | 读取 `Retry-After` header，等待后重试 |
| 返回非 JSON | 尝试剥离 markdown 代码块（` ```json ... ``` `），仍失败则重试 1 次（prompt 末尾追加"请只输出纯 JSON"） |
| JSON 结构不匹配 | 宽松反序列化（忽略未知字段），缺失必填字段时用空值补全，日志 warn |
| 分批生成中某批失败 | 已完成的批次结果实时写入 `llm_cache`（每批完成即 `SaveBatchCache`），失败批次记录 `status=failed` + `error_msg`，重试 2 次仍失败则跳过，输出中标注 `[生成失败，请手动补充]`，session 状态置为 `partial_generated` |

### 生成中途崩溃恢复

- LLM 原始输出按批次实时写入 `llm_cache` 表（每批完成即写入，`batch_index` 区分）
- 重新运行 `apidoc generate` 时，检测到 `status=generating` 或 `partial_generated` 的 session，提示用户：
  - `--resume`：从缓存恢复，只重新生成 `status=failed` 的批次（通过 `GetFailedBatches` 查询）
  - `--no-cache`：调用 `ClearCaches` 丢弃所有批次缓存，全部重新生成
- Session 状态机：`imported → generating → generated | partial_generated | failed`
  - `generated`：所有批次成功
  - `partial_generated`：部分批次成功，部分跳过（文档可用但不完整）
  - `failed`：全部批次失败或致命错误

### 插件端错误处理

- 后端不可达：插件显示"无法连接后端，请确认 apidoc serve 已启动"
- 生成超时（>5min 无状态变化）：插件停止轮询，显示"生成超时，请检查后端日志"

---

## 测试策略

每个 milestone 包含对应的测试任务，测试代码放在各模块同目录下 `*_test.go`。

- **单元测试**：每个模块核心函数必须有测试，覆盖正常路径 + 边界情况
- **集成测试**：M2 完成后增加端到端测试（HAR → 文档）
- **测试数据**：`testdata/` 目录存放 HAR 样本、预期输出等
- **CI**：`go test ./...` + `go vet` + `golangci-lint`

---

## M1：HAR 解析 + 存储 + CLI 骨架 + Config（3-4 天）

### 任务 1.1：项目初始化

- `go mod init github.com/yourorg/apidoc`
- 依赖：`cobra`, `modernc.org/sqlite`, `gopkg.in/yaml.v3`, `goldmark`
- 创建目录结构
- 验收：`go build ./...` 通过

### 任务 1.2：Config 模块

`internal/config/config.go`

```go
type Config struct {
    LLM      LLMConfig      `yaml:"llm"`
    Output   OutputConfig   `yaml:"output"`
    Filter   FilterConfig   `yaml:"filter"`
    Sanitize SanitizeConfig `yaml:"sanitize"`
    Server   ServerConfig   `yaml:"server"`
    Log      LogConfig      `yaml:"log"`
}

// Load 加载配置：~/.apidoc/config.yaml → 环境变量覆盖 → 命令行参数覆盖
func Load(configPath string) (*Config, error)

// 填充默认值：model=gpt-4o, port=3000, host=127.0.0.1, level=info
func (c *Config) SetDefaults()

// 校验：api_key 不能为空（generate 时）、output.dir 可写
func (c *Config) Validate() error
```

验收：无配置文件时用默认值启动，有配置文件时正确加载

### 任务 1.3：CLI 骨架（cobra）

`cmd/apidoc/main.go`

```go
// 子命令：
// apidoc init                    // 初始化 ~/.apidoc/ 目录 + 默认 config.yaml
// apidoc generate --har <file> --scenario <text> [--no-cache] [--resume] [--config <path>]
// apidoc import   --har <file> [--scenario <text>]
// apidoc serve    [--port 3000] [--host 127.0.0.1]
// apidoc list                    // 列出所有 session
// apidoc show     --session <id> [--version <n>]  // 查看 session 详情（指定版本）
// apidoc delete   --session <id> // 删除 session
//
// 全局 flags：--verbose, --debug, --config
//
// V2 预留（MVP 不实现）：
// apidoc diff     --session <id> --v1 <n> --v2 <n>  // 对比两个版本的 OpenAPI diff
```

`apidoc init` 实现要点：
- 创建 `~/.apidoc/` 目录
- 生成默认 `config.yaml`（api_key 留空，提示用户填写）
- 初始化 SQLite 数据库
- 幂等：已存在则跳过，不覆盖

验收：`apidoc --help` 显示所有子命令

### 任务 1.4：HAR 解析器

`internal/har/parser.go`

```go
type HARFile struct {
    Log struct {
        Entries []Entry `json:"entries"`
    } `json:"log"`
}

// Parse 解析 HAR 文件，返回 []types.TrafficLog
func Parse(filePath string) ([]types.TrafficLog, error)
```

实现要点：
- 时间解析：HAR 用 ISO 8601 格式
- query params 从 URL 解析（`net/url`），类型为 `map[string][]string` 支持同名多值
- response body：检查 `content.encoding`，如果是 `"base64"` 则解码
- 二进制 body（图片等）：标记 `RequestBodyEncoding: "omitted"`，不存内容
- 按 `startedDateTime` 排序，赋值 seq

验收：准备含 base64 body 的 HAR 文件，解析后字段完整无丢失

### 任务 1.5：SQLite 存储层

`internal/store/store.go`

```go
type Store interface {
    // Session
    CreateSession(source, scenario, host string) (*types.Session, error)
    GetSession(id string) (*types.Session, error)
    UpdateSessionStatus(id, status string) error
    ListSessions() ([]types.Session, error)
    DeleteSession(id string) error  // 级联删除 logs + cache

    // Traffic
    SaveLogs(sessionID string, logs []types.TrafficLog) error
    GetLogs(sessionID string) ([]types.TrafficLog, error)

    // LLM Cache（批次级）
    SaveBatchCache(cache *types.LLMCache) error
    GetBatchCaches(sessionID string) ([]types.LLMCache, error)
    GetFailedBatches(sessionID string) ([]types.LLMCache, error)  // --resume 用
    ClearCaches(sessionID string) error                           // --no-cache 用

    Close() error
}
```

`internal/store/sqlite.go` 实现要点：
- 使用 `modernc.org/sqlite`（纯 Go，无 CGO）
- 启用 WAL 模式：`PRAGMA journal_mode=WAL`
- `Init()` 自动建表 + 建索引（`idx_traffic_session`）
- Session ID：`sess_` + 日期 + 自增序号（`sess_20260224_001`）
- SaveLogs 用事务批量插入
- DeleteSession 级联删除 traffic_logs 和 llm_cache
- JSON 字段用 `json.Marshal` 序列化存为 TEXT

验收：单元测试覆盖 CRUD + 删除级联 + 缓存读写

### 任务 1.6：M1 测试

- `internal/config/config_test.go`：默认值填充、YAML 加载、校验逻辑
- `internal/har/parser_test.go`：正常 HAR、base64 body、空 entries、畸形 JSON
- `internal/store/sqlite_test.go`：CRUD、级联删除、并发读写（WAL）
- 测试数据：`testdata/sample.har`、`testdata/base64-body.har`、`testdata/empty.har`

---

## M2：流量过滤 + 脱敏 + LLM 文档生成 + 缓存（1.5 周）

### 🚩 M2 检查点：Week 2 第3天，LLM 能返回有效 JSON（即使质量不高）

### 任务 2.1：流量过滤器

`internal/filter/filter.go`

```go
func Apply(logs []types.TrafficLog, cfg FilterConfig) []types.TrafficLog

// 过滤规则（按优先级）：
// 1. 去掉 OPTIONS 预检请求
// 2. 根据 path 后缀过滤静态资源
// 3. 根据 response content-type 过滤非 API 响应
// 4. 根据 ignore_paths 配置过滤指定路径前缀
// 5. 合并完全相同的请求（method + path + query 都相同），记录 CallCount
//    ⚠️ 不合并"相同 path 不同参数"的请求，保留所有不同参数组合
// 6. 去掉连续相同 API 的 5xx 重试（保留首次）
```

验收：输入 50 条混合流量，过滤后只剩 API 请求，同参数请求合并且 CallCount 正确

### 任务 2.2：敏感数据脱敏

`internal/filter/sanitize.go`

```go
func Sanitize(logs []types.TrafficLog, cfg SanitizeConfig) []types.TrafficLog

// 脱敏逻辑：
// 1. Header 脱敏：匹配 sensitiveHeaders，值替换为 replacement
// 2. Body 脱敏：JSON body 递归遍历 map[string]interface，字段名匹配则替换
//    支持嵌套结构（递归处理）
// 3. Query 脱敏：参数名匹配 sensitiveFields 则替换值
// 4. 字段名匹配不区分大小写
// 5. 非 JSON body 跳过
```

验收：构造含嵌套敏感字段的请求，脱敏后所有敏感值被替换，非敏感字段不变

### 任务 2.3：LLM 客户端

`internal/generator/llm.go`

```go
type Client struct {
    BaseURL     string
    APIKey      string
    Model       string
    MaxTokens   int
    Temperature float64
    HTTPClient  *http.Client
    Logger      *slog.Logger
}

func (c *Client) Chat(systemPrompt, userPrompt string) (string, error)
func (c *Client) ChatJSON(systemPrompt, userPrompt string, out interface{}) error
```

实现要点：
- 兼容 OpenAI `/v1/chat/completions` 接口
- ChatJSON：调用 Chat → 剥离 markdown 代码块包裹 → `json.Unmarshal`
- 超时 120s
- 重试：429/5xx 重试 3 次，指数退避（1s → 2s → 4s）
- `--debug` 模式下输出完整 prompt 和 response 到日志

验收：能成功调用 LLM API，返回结构化 JSON

### 任务 2.4：Token 预估 + 分批策略

`internal/generator/batcher.go`

```go
// EstimateTokens 粗略预估 prompt token 数（中文约 2 char/token，英文约 4 char/token）
func EstimateTokens(text string) int

// ShouldBatch 判断是否需要分批
func ShouldBatch(logs []types.TrafficLog, maxTokens int) bool

// SplitBatches 按 path 前缀分组，每批不超过 maxTokens
func SplitBatches(logs []types.TrafficLog, maxTokens int) [][]types.TrafficLog

// MergeDocs 合并多批生成的文档，按 method+path 去重
func MergeDocs(docs []*types.GeneratedDoc) *types.GeneratedDoc
```

分批策略：
- 按 path 第一段分组（如 `/api/v1/namespaces/*` 为一组）
- 每批独立生成 endpoints
- call_chain 在最后一批中传入所有 endpoint 摘要，统一生成
- 合并时：endpoints 按 method+path 去重，call_chain 取最后一批的结果

验收：30+ 条流量能正确分批，合并后文档无重复 endpoint

### 任务 2.5：Prompt 模板

`internal/generator/prompt.go`

System Prompt 要点（完整内容见架构文档）：
- 明确输出语言：中文描述，英文字段名
- 类型推断规则：UUID→string(uuid)、ISO8601→string(datetime)、整数→integer、小数→number、布尔→boolean
- 要求输出 Tags 分组
- 要求嵌套字段用 children 表达
- 同一 API 多次调用时合并为一个端点
- 末尾重复强调：只输出 JSON，不要 markdown 代码块

User Prompt 包含 few-shot 示例（见架构文档）。

```go
func BuildUserPrompt(scenario string, logs []types.TrafficLog) string
```

实现要点：
- body 截断策略：>2000 字符时保留 JSON 第一层所有 key，截断深层嵌套值
- 如果流量条数 > 30 且未分批，按 path 去重后发送
- 标注 CallCount > 1 的请求："此 API 被调用了 N 次"

验收：手动构造 prompt 发给 LLM，返回的 JSON 能正确反序列化为 GeneratedDoc

### 任务 2.6：文档渲染器

`internal/generator/renderer.go`

```go
// RenderMarkdown 渲染为 Markdown
// 输出：README.md（场景+调用链）+ api-docs.md（端点文档）
func RenderMarkdown(doc *types.GeneratedDoc, outputDir string) error

// RenderOpenAPI 渲染为 OpenAPI 3.0 YAML
// 输出：openapi.yaml
// 使用 Endpoint.Tags 生成 OpenAPI tags 分组
// Param.Children 递归生成嵌套 schema
func RenderOpenAPI(doc *types.GeneratedDoc, outputDir string) error

// ValidateOpenAPI 校验生成的 OpenAPI spec 格式合法性
func ValidateOpenAPI(yamlPath string) []string
```

OpenAPI 渲染要点：
- Tags 映射到 OpenAPI 的 tags 字段
- Param.Children 递归生成 `properties` 嵌套
- Response.ContentType 映射到 `content` 的 media type
- 每个 endpoint 的 example 放在 schema 里

验收：Markdown 可读，OpenAPI YAML 通过 ValidateOpenAPI 校验

### 任务 2.7：Generator 编排

`internal/generator/generator.go`

```go
type ProgressFunc func(stage string)

func Generate(sess *types.Session, logs []types.TrafficLog, 
    llmCfg LLMConfig, store Store, onProgress ProgressFunc, noCache bool, resume bool) (*types.GeneratedDoc, error) {
    
    // 1. 检查缓存（除非 --no-cache）
    if noCache {
        store.ClearCaches(sess.ID)
    }
    
    // 2. 判断是否需要分批
    batches := [][]types.TrafficLog{logs}
    if ShouldBatch(logs, llmCfg.MaxTokens) {
        batches = SplitBatches(logs, llmCfg.MaxTokens)
    }
    
    // 3. 逐批生成（支持 --resume 跳过已成功批次）
    existingCaches, _ := store.GetBatchCaches(sess.ID)
    var allDocs []*types.GeneratedDoc
    hasFailure := false
    
    for i, batch := range batches {
        // --resume 模式：跳过已成功的批次
        if resume && batchSucceeded(existingCaches, i) {
            onProgress(fmt.Sprintf("batch %d/%d: using cache", i+1, len(batches)))
            allDocs = append(allDocs, parseCachedDoc(existingCaches, i))
            continue
        }
        
        onProgress(fmt.Sprintf("batch %d/%d: calling LLM...", i+1, len(batches)))
        doc, err := callLLM(batch, llmCfg)
        
        // 每批完成即写入缓存
        cache := &types.LLMCache{SessionID: sess.ID, BatchIndex: i, ...}
        if err != nil {
            cache.Status = "failed"
            cache.ErrorMsg = err.Error()
            hasFailure = true
        } else {
            cache.Status = "ok"
            cache.RawOutput = rawJSON
            allDocs = append(allDocs, doc)
        }
        store.SaveBatchCache(cache)
    }
    
    // 4. 合并 + 后处理
    merged := MergeDocs(allDocs)
    postProcess(merged)
    
    // 5. 更新 session 状态
    if hasFailure {
        store.UpdateSessionStatus(sess.ID, "partial_generated")
    } else {
        store.UpdateSessionStatus(sess.ID, "generated")
    }
    
    return merged, nil
}
```

验收：端到端 — HAR 文件 + 场景描述 → 完整 Markdown + OpenAPI 文档

### 任务 2.8：M2 测试

- `internal/filter/filter_test.go`：OPTIONS 过滤、静态资源过滤、同参数合并 CallCount、5xx 去重
- `internal/filter/sanitize_test.go`：header 脱敏、嵌套 body 脱敏、非 JSON body 跳过、大小写不敏感
- `internal/generator/llm_test.go`：mock HTTP server 模拟 LLM 响应、重试逻辑、markdown 代码块剥离
- `internal/generator/batcher_test.go`：token 预估、分批边界、合并去重
- `internal/generator/renderer_test.go`：Markdown 输出格式、OpenAPI YAML 校验、嵌套 schema
- **集成测试** `internal/generator/generator_integration_test.go`：
  - 用 `testdata/sample.har` 跑完整流程（可用 mock LLM）
  - 验证缓存命中 / `--no-cache` 跳过缓存
  - 验证分批生成 + 合并
  - 验证 `--resume` 恢复失败批次

---

## M3：Chrome Extension（1.5 周）

### 任务 3.1：插件基础结构

`extension/manifest.json`（见架构文档，含 `unlimitedStorage` 权限）

核心文件：
- `manifest.json` — MV3 配置
- `background.js` — Service Worker，popup↔devtools 通信中转
- `devtools.html/js` — 创建 DevTools panel
- `panel.html/js` — 录制控制 + 请求列表展示
- `popup.html/js` — 快捷状态查看 + 场景描述 + 导出/生成

验收：插件加载到 Chrome，DevTools 中出现 "API Recorder" panel

### 任务 3.2：DevTools 网络监听

`extension/panel.js`

```javascript
let recording = false;
const STORAGE_KEY = 'apidoc_captured_requests';

chrome.devtools.network.onRequestFinished.addListener((request) => {
  if (!recording) return;
  if (shouldIgnore(request)) return;
  
  // 用 Promise 队列保证高并发时的顺序
  requestQueue = requestQueue.then(() => {
    return new Promise((resolve) => {
      request.getContent((body) => {
        const entry = buildEntry(request, body);
        // 立即写入 chrome.storage.local，不依赖 SW 内存
        appendToStorage(STORAGE_KEY, entry).then(resolve);
      });
    });
  });
});
```

⚠️ MV3 关键点：
- 数据每条实时写入 `chrome.storage.local`，防止 Service Worker 被回收丢数据
- `getContent()` 是异步的，用 Promise 链保证顺序
- 需要 DevTools 保持打开才能监听

验收：录制状态下能捕获 API 请求，关闭再打开 DevTools 数据不丢失

### 任务 3.3：Panel 控制界面

`extension/panel.html` + `panel.js`

```
┌─────────────────────────────┐
│  🔴 API Doc Recorder        │
│                             │
│  状态：未录制 / 录制中 (12条) │
│  ⚠️ 请保持 DevTools 打开     │
│                             │
│  [ 开始录制 ]  [ 停止录制 ]   │
│                             │
│  已捕获请求：                │
│  ┌─────────────────────────┐│
│  │ GET  /api/v1/users  200 ││
│  │ POST /api/v1/users  201 ││
│  │ ...                     ││
│  └─────────────────────────┘│
│                             │
│  场景描述：                  │
│  ┌─────────────────────────┐│
│  │                         ││
│  └─────────────────────────┘│
│                             │
│  后端地址：http://127.0.0.1:3000 │
│  [ 导出 HAR ]  [ 生成文档 ]  │
└─────────────────────────────┘
```

验收：完整录制→停止→导出/生成流程可走通

### 任务 3.4：后端异步 API

`internal/server/api.go`

```go
// POST /api/generate — 异步触发文档生成
type GenerateRequest struct {
    Scenario string             `json:"scenario"`
    Logs     []types.TrafficLog `json:"logs"`
}
type GenerateResponse struct {
    SessionID string `json:"session_id"`
    Status    string `json:"status"` // "generating"
}

// GET /api/sessions/:id — 查询生成状态
type SessionResponse struct {
    Session    types.Session `json:"session"`
    PreviewURL string        `json:"preview_url,omitempty"` // 生成完成后返回
}

// GET /api/sessions — 列表
// DELETE /api/sessions/:id — 删除
```

实现要点：
- `POST /api/generate` 接收数据后立即返回 session_id，后台 goroutine 执行生成
- CORS 白名单允许具体的 `chrome-extension://<extension-id>` origin（ID 从 `config.yaml` 的 `server.cors_extension_id` 读取，未配置时拒绝所有 extension origin 并日志 warn）
- 插件端轮询 `GET /api/sessions/:id` 查看状态，生成完成后自动打开预览

验收：curl 模拟插件请求，异步生成完成后状态变为 "generated"

### 任务 3.5：M3 测试

- `internal/server/api_test.go`：POST /api/generate 异步返回、GET 状态轮询、CORS header、DELETE 级联
- 插件手动测试 checklist：
  - [ ] 录制 → 停止 → 请求列表正确
  - [ ] 关闭 DevTools 再打开，数据不丢失（chrome.storage 持久化）
  - [ ] 高并发页面（20+ 请求同时）录制顺序正确
  - [ ] 导出 HAR 文件可被 `apidoc import` 正常解析
  - [ ] 生成文档 → 轮询状态 → 完成后显示预览链接
  - [ ] 后端未启动时显示友好错误提示

---

## M4：本地预览服务（3-4 天）

### 任务 4.1：文档预览 HTTP 服务

`internal/server/preview.go`

```go
func Serve(store Store, outputDir string, cfg ServerConfig) error

// 路由：
// GET /                       → session 列表页
// GET /sessions/:id           → Markdown 渲染页
// GET /sessions/:id/openapi   → OpenAPI YAML 下载
// GET /sessions/:id/swagger   → 内嵌 Swagger UI
```

实现要点：
- Markdown → HTML：用 `goldmark` 渲染，内嵌简单 CSS
- Swagger UI：用 CDN 引入（`unpkg.com/swagger-ui-dist`），减小二进制体积
- 默认绑定 `127.0.0.1`
- session 列表显示：ID、场景、host、记录数、状态、时间

验收：`apidoc serve` 启动后浏览器能查看文档，Swagger UI 能加载 OpenAPI spec

### 任务 4.2：M4 测试

- `internal/server/preview_test.go`：路由正确性、Markdown 渲染、OpenAPI 下载 content-type、Swagger UI 页面加载
- 手动测试：不同浏览器（Chrome/Firefox）下 Swagger UI 渲染正常

---

## M5：内部 Dogfood + Prompt 调优（1-1.5 周）

### 任务 5.1：真实场景测试矩阵

| # | 场景 | 复杂度 | 关注点 | 通过标准 |
|---|------|--------|--------|----------|
| 1 | 简单 CRUD（创建/删除单个资源） | 低 | 基本端点识别、参数类型推断 | 端点完整、类型正确 |
| 2 | 列表查询 + 分页 | 低 | 分页参数识别、响应嵌套结构 | 分页参数标注、items 嵌套正确 |
| 3 | 复杂链路（创建集群→添加节点→部署） | 高 | 调用链依赖关系、数据流转 | call_chain 依赖正确、ID 传递标注 |
| 4 | 含文件上传的场景 | 中 | multipart body 处理 | body 标记为 binary/omitted，不报错 |
| 5 | 同一 API 不同参数多次调用 | 中 | 合并策略、参数组合 | 合并为一个端点，列出所有参数组合 |
| 6 | 含 401/403 错误响应 | 中 | 多状态码 response | 正确列出多个 response status |
| 7 | 大量 API（30+ 条流量） | 高 | 分批生成 + 合并 | 无重复端点，call_chain 完整 |
| 8 | 非 RESTful API（RPC 风格） | 中 | path 相同但 body 不同 | 正确区分不同操作 |

每个场景记录：输入流量条数、生成耗时、token 消耗、文档质量评分（1-5）、具体问题。

### 任务 5.2：Prompt 迭代优化

根据 dogfood 反馈调整（每轮迭代记录变更和效果）：

| 问题 | 优化方向 | 验证方式 |
|------|----------|----------|
| 参数类型推断不准 | 强化类型推断规则，增加更多示例 | 对比同一流量优化前后的类型准确率 |
| 字段描述太泛 | 要求结合字段名+值+上下文推断含义 | 人工评审描述质量 |
| 调用链关系不清 | 要求标注数据依赖（"步骤2用了步骤1返回的 ID"） | 检查 depends_on 是否正确 |
| 中英文混杂 | 强化语言指令，末尾重复强调 | 检查输出中文描述比例 |
| Tags 分组不合理 | 提供分组示例，按资源类型分组 | 导入 Swagger UI 检查分组效果 |
| 嵌套结构丢失 | 强调 children 递归，给嵌套示例 | 对比原始 response 和生成的 schema 层级 |

Prompt 版本管理：每次修改 prompt 记录在 `prompts/changelog.md`，便于回溯。

### 任务 5.3：边界情况处理

| 场景 | 处理方式 | 验收 |
|------|----------|------|
| 超大 response body（>10KB） | 保留第一层 key，截断深层值，标注 `[truncated]` | 不报错，schema 第一层完整 |
| 非 JSON API（文件上传/下载） | body 标记为 `omitted`，content-type 记录 | 端点正确识别，body 不乱码 |
| WebSocket 请求 | 跳过，日志 info 提示 | 不影响其他请求处理 |
| 分页 API 多次调用 | 合并为一个端点，标注分页参数 | 只生成一个端点，参数完整 |
| 空 response body（204） | 正确处理，response 无 fields | 不报错，status_code 正确 |
| 超大 HAR 文件（>50MB） | 流式解析，内存不超过 200MB | 不 OOM |
| LLM 返回截断的 JSON | 检测不完整 JSON，自动重试并降低 body 长度 | 最终拿到完整输出或明确报错 |

验收：8 个真实场景中 5+ 个生成的文档达到"可直接使用"水平（质量评分 ≥ 4）

### 任务 5.4：生成文档版本管理

同一 session 多次生成文档时，保留历史版本：

```
output/
├── sess_20260224_001/
│   ├── v1/
│   │   ├── api-docs.md
│   │   ├── openapi.yaml
│   │   └── meta.json          # {version, model, tokens, timestamp, prompt_version}
│   ├── v2/
│   │   ├── api-docs.md
│   │   ├── openapi.yaml
│   │   └── meta.json
│   ├── latest -> v2/          # Unix 软链接（首选）
│   └── latest.json            # 跨平台 fallback：{"version": 2, "path": "v2"}
```

- 版本指针策略：优先创建软链接，失败时（Windows / 权限不足）回退到 `latest.json` 文件
- 代码中统一通过 `resolveLatest(sessionDir)` 读取最新版本路径，内部先检查软链接再检查 `latest.json`

- `apidoc generate` 默认生成新版本，`latest` 软链接自动更新
- `apidoc show --session <id> --version <n>` 查看指定版本
- `apidoc diff --session <id> --v1 1 --v2 2` 对比两个版本的 OpenAPI diff（V2 可选）
- `meta.json` 记录：版本号、使用的模型、token 消耗、生成时间、prompt 版本
