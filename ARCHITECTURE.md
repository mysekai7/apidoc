# API Doc Assistant — MVP 架构设计 v3.1

## 产品定位

从实际运行的 API 流量中，结合场景描述，自动生成和维护 API 文档。

**核心理念：文档来自真实流量，不是手写猜测。**
**核心原则：零侵入，不碰产品任何配置。**

## MVP 范围

最小闭环：

1. 用户通过浏览器插件录制 / 导入 HAR 文件
2. 告诉 AI 这个场景是什么
3. AI 分析流量，生成 Markdown + OpenAPI 3.0 文档

暂不做：持续监听、漂移检测、反向代理模式、eBPF、跨 session 文档合并（V2）

## 整体架构

```
┌──────────────────────────────────────────────────┐
│                  采集层（二选一）                    │
│                                                    │
│  ┌─────────────────┐    ┌──────────────────────┐  │
│  │ Chrome Extension │    │  HAR 文件导入         │  │
│  │ 一键录制/停止     │    │  F12 导出 → CLI 导入  │  │
│  └────────┬────────┘    └──────────┬───────────┘  │
│           │                        │               │
│           └──────────┬─────────────┘               │
└──────────────────────┼─────────────────────────────┘
                       │ 标准化流量数据
                       ▼
              ┌──────────────────┐
              │  Config          │
              │  (YAML 配置管理)  │
              └────────┬─────────┘
                       │
                       ▼
              ┌──────────────────┐
              │  Traffic Store   │
              │  (SQLite WAL)    │
              └────────┬─────────┘
                       │
                       ▼
              ┌──────────────────┐
              │  Filter + 脱敏   │
              └────────┬─────────┘
                       │
                       ▼
              ┌──────────────────┐
              │  Doc Generator   │
              │  (LLM + Cache)   │
              └────────┬─────────┘
                       │
                       ▼
              ┌──────────────────┐
              │  Output          │
              │  Markdown/OpenAPI│
              └──────────────────┘
```

## 核心模块

### 1. Config（配置管理）

- **职责**：加载、校验、填充默认值
- **文件**：`~/.apidoc/config.yaml`
- **实现**：`internal/config/config.go`

### 2. Chrome Extension（浏览器插件）

- **职责**：一键录制浏览器中的 API 请求/响应
- **技术**：`chrome.devtools.network.onRequestFinished` 监听请求
- **⚠️ 限制**：需要保持 DevTools 面板打开才能录制（MV3 限制）
- **交互流程**：
  1. 用户打开 DevTools → 切到 API Recorder panel
  2. 点击 "开始录制"，正常操作产品
  3. 点击 "停止录制"
  4. 输入场景描述
  5. 选择：导出 HAR 文件 / 直接发送到 apidoc 后端
- **过滤**：自动忽略静态资源（js/css/img/font），只保留 API 请求
- **数据持久化**：每次捕获请求后立即写入 `chrome.storage.local`，不依赖 Service Worker 内存（防止 SW 被回收导致数据丢失）
- **通信架构**：popup ↔ Service Worker（background.js）↔ DevTools panel（不能直接 popup→devtools）

### 3. HAR 导入（CLI）

- **职责**：解析标准 HAR 文件，转为内部流量格式
- **来源**：浏览器 F12 → Network → Save all as HAR
- **实现**：Go 解析 HAR JSON，支持 base64 编码的 response body（`content.encoding: "base64"`）
- **命令**：`apidoc import --har ./recording.har --scenario "创建命名空间"`

### 4. Traffic Store（存储层）

- **SQLite WAL 模式**，支持并发读写
- 采集层数据统一转为内部格式后入库

```sql
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    source      TEXT NOT NULL,        -- 'extension' | 'har'
    scenario    TEXT,
    host        TEXT,                 -- 目标服务 host
    log_count   INTEGER DEFAULT 0,    -- 流量记录条数
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    status      TEXT DEFAULT 'imported'  -- imported | generating | generated | partial_generated | failed
);

CREATE TABLE traffic_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id      TEXT NOT NULL REFERENCES sessions(id),
    seq             INTEGER NOT NULL,
    timestamp       DATETIME NOT NULL,
    method          TEXT NOT NULL,
    path            TEXT NOT NULL,
    host            TEXT,
    query_params    TEXT,              -- JSON（map[string][]string，支持同名多值）
    request_headers TEXT,              -- JSON（已脱敏）
    request_body    TEXT,              -- JSON（已脱敏）
    request_body_encoding TEXT,        -- 'plain' | 'base64' | 'omitted'
    content_type    TEXT,
    status_code     INTEGER NOT NULL,
    response_headers TEXT,             -- JSON
    response_body   TEXT,              -- JSON
    response_content_type TEXT,
    latency_ms      INTEGER
);

CREATE INDEX idx_traffic_session ON traffic_logs(session_id);

-- LLM 原始输出缓存（批次级，支持 --resume 只重跑失败批次）
CREATE TABLE llm_cache (
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    batch_index INTEGER NOT NULL,      -- 批次序号（0-based，单批时为 0）
    batch_key   TEXT,                  -- 批次标识（如 path 前缀分组名）
    status      TEXT NOT NULL DEFAULT 'ok',  -- 'ok' | 'failed'
    raw_output  TEXT,                  -- LLM 原始 JSON 输出（失败时为 NULL）
    model       TEXT,
    tokens_used INTEGER,
    error_msg   TEXT,                  -- 失败时的错误信息
    created_at  DATETIME NOT NULL,
    PRIMARY KEY (session_id, batch_index)
);
```

### 5. Filter（过滤 + 脱敏）

- **智能过滤规则**：
  - 去掉 OPTIONS 预检请求
  - 根据 path 后缀 / content-type 过滤静态资源
  - 合并完全相同的请求（method + path + query params 都相同），标注调用次数
  - 忽略连续相同 API 的 5xx 重试（保留首次）
  - ⚠️ 不再激进合并"相同 path 不同参数"的请求，保留所有不同参数组合

- **脱敏**：header / body / query 中的敏感字段替换为 `***REDACTED***`

### 6. Doc Generator（文档生成层）

- **流程**：
  1. 检查 llm_cache 是否有缓存，有则跳过已成功的批次（支持 `--no-cache` 强制全部重新生成，`--resume` 只重跑失败批次）
  2. 从 store 拉取 session 的流量记录
  3. 过滤去噪 + 脱敏
  4. 组装 prompt（场景描述 + 流量数据）
  5. Token 预估，超限则分批（按 API 端点分组，每批独立生成，最后合并）
  6. LLM 输出结构化 JSON，缓存原始输出
  7. 后处理：校验、补全、去重
  8. 渲染为 Markdown + OpenAPI 3.0 YAML
  9. OpenAPI 输出后用内置校验器检查格式合法性

- **分批合并策略**：
  - 按 path 前缀分组（如 `/api/v1/namespaces/*` 为一组）
  - 每批独立生成 endpoints，call_chain 在最后一批中统一生成
  - 合并时按 method+path 去重

- **进度回调**：支持 `onProgress(stage string)` 回调，CLI 显示进度，插件端显示状态

### 7. Server（HTTP 服务）

职责拆分为两部分：

- **API 服务**（`internal/server/api.go`）：接收插件数据，异步触发文档生成
  - `POST /api/generate` → 返回 `{session_id, status: "generating"}`（异步）
  - `GET /api/sessions` → session 列表
  - `GET /api/sessions/:id` → session 详情 + 生成状态
  - CORS 白名单允许具体的 `chrome-extension://<extension-id>` origin（extension ID 在首次安装后固定，配置在 `config.yaml` 的 `server.cors_extension_id` 字段）

- **预览服务**（`internal/server/preview.go`）：文档浏览
  - `GET /` → session 列表页
  - `GET /sessions/:id` → Markdown 渲染页
  - `GET /sessions/:id/openapi` → OpenAPI YAML 下载
  - `GET /sessions/:id/swagger` → 内嵌 Swagger UI

- **默认绑定 `127.0.0.1`**，避免局域网暴露

### 8. 日志与可观测性

- CLI 支持 `--verbose` / `--debug` 级别控制
- `--debug` 模式下输出完整的 LLM prompt 和 response（方便调试 prompt）
- 使用 `log/slog`（Go 1.21+ 标准库），结构化日志

### 9. 文档版本管理

同一 session 多次生成文档时，保留历史版本而非覆盖：

```
output/
├── sess_20260224_001/
│   ├── v1/
│   │   ├── api-docs.md
│   │   ├── openapi.yaml
│   │   └── meta.json       # {version, model, tokens, timestamp, prompt_version}
│   ├── v2/
│   │   └── ...
│   ├── latest -> v2/        # Unix 软链接（首选）
│   └── latest.json          # 跨平台 fallback：{"version": 2, "path": "v2"}
```

- 每次 `apidoc generate` 生成新版本目录，`latest` 指针自动更新
- 版本指针策略：优先软链接，失败时（Windows / 权限不足）回退到 `latest.json`
- 代码统一通过 `resolveLatest(sessionDir)` 读取最新版本路径
- `meta.json` 记录版本号、模型、token 消耗、生成时间、prompt 版本
- `apidoc show --session <id> --version <n>` 查看指定版本

## LLM Prompt 设计

**System Prompt：**

```
你是一个 API 文档专家。你会收到：
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
```

**User Prompt 模板：**

```
## 场景描述
{scenario}

## API 调用记录（共 {count} 条，按时间排序）
{traffic_records_json}

## 输出示例（仅供参考格式）
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

请分析以上流量，生成该场景的完整 API 文档。
```

## 项目结构

```
apidoc/
├── cmd/
│   └── apidoc/
│       └── main.go              # CLI 入口（cobra）
├── internal/
│   ├── config/
│   │   └── config.go            # 配置加载、校验、默认值
│   ├── har/
│   │   └── parser.go            # HAR 文件解析（含 base64 body）
│   ├── store/
│   │   ├── store.go             # 存储接口
│   │   └── sqlite.go            # SQLite WAL 实现
│   ├── filter/
│   │   ├── filter.go            # 流量过滤（去噪、去重）
│   │   └── sanitize.go          # 敏感数据脱敏
│   ├── generator/
│   │   ├── generator.go         # 文档生成编排 + 进度回调
│   │   ├── llm.go               # LLM API 客户端
│   │   ├── prompt.go            # Prompt 模板
│   │   ├── batcher.go           # Token 预估 + 分批策略
│   │   └── renderer.go          # JSON → Markdown / OpenAPI
│   └── server/
│       ├── api.go               # 接收插件数据的 API（异步生成）
│       └── preview.go           # 本地文档预览
├── pkg/
│   └── types/
│       └── types.go             # 公共数据结构
├── extension/                   # Chrome 插件
│   ├── manifest.json
│   ├── popup.html / popup.js
│   ├── panel.html / panel.js    # DevTools panel UI
│   ├── devtools.html / devtools.js
│   └── background.js            # Service Worker 中转
├── output/
├── go.mod
└── go.sum
```

## Chrome Extension 设计（MV3）

### manifest.json

```json
{
  "manifest_version": 3,
  "name": "API Doc Recorder",
  "version": "0.1.0",
  "permissions": ["storage", "unlimitedStorage"],
  "devtools_page": "devtools.html",
  "action": {
    "default_popup": "popup.html",
    "default_icon": "icon.png"
  },
  "background": {
    "service_worker": "background.js"
  }
}
```

### 通信架构（MV3 限制：popup 不能直接与 devtools 通信）

```
popup.html                 background.js              devtools.js / panel.js
┌──────────┐              ┌──────────────┐           ┌──────────────────┐
│ [录制]    │──message──▶ │ Service Worker│──message─▶│ 开始/停止监听     │
│ [停止]    │             │ 状态中转      │           │ network requests │
│ [场景描述]│             │ 数据暂存      │◀─data────│ 每条请求立即写入  │
│ [导出]    │◀──status──  │ chrome.storage│           │ chrome.storage   │
│ [生成]    │             └──────────────┘           └──────────────────┘
└──────────┘
```

### ⚠️ MV3 关键注意事项

1. **Service Worker 会被回收**：空闲 ~30s 后 Chrome 会杀掉 SW。录制数据必须实时写入 `chrome.storage.local`，不能只存内存
2. **DevTools 必须打开**：`onRequestFinished` 只在 DevTools 打开时工作，需在 UI 中提示用户
3. **`unlimitedStorage` 权限**：默认 `chrome.storage.local` 限制 5MB，大量流量会超限
4. **异步 `getContent()`**：高并发请求时用 Promise 队列保证顺序

## 关键技术决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 采集方式 | Chrome Extension + HAR 导入 | 零侵入，不碰产品配置 |
| 语言 | Go（后端）+ JS（插件） | 团队主力 + 插件必须 JS |
| 存储 | SQLite WAL 模式 | 零依赖，支持并发读写 |
| SQLite 驱动 | modernc.org/sqlite | 纯 Go，无 CGO，交叉编译友好 |
| CLI 框架 | cobra | Go 生态标准 |
| 日志 | log/slog | Go 1.21+ 标准库，结构化日志 |
| LLM | 兼容 OpenAI API 的任意模型 | 灵活切换，不绑定厂商 |
| 输出格式 | Markdown + OpenAPI 3.0 | 可读 + 可导入工具链 |
| Server 绑定 | 127.0.0.1 | 默认不暴露到局域网 |

## MVP 里程碑

| 阶段 | 内容 | 预估时间 |
|------|------|----------|
| M1 | HAR 解析 + 存储 + CLI 骨架 + Config | 3-4 天 |
| M2 | 流量过滤 + 脱敏 + LLM 文档生成 + 缓存 | 1.5 周 |
| M2 检查点 | Week 2 第3天：LLM 能返回有效 JSON | — |
| M3 | Chrome Extension 录制 + 导出 | 1.5 周 |
| M4 | OpenAPI 输出 + 本地预览 + Swagger UI | 3-4 天 |
| M5 | 内部 dogfood + prompt 调优 + 边界处理 | 1-1.5 周 |

**总计约 5-6 周。**

优先级：M1 → M2 先跑通 HAR 导入 + 文档生成的核心链路，再做插件。

## V2 路线图（MVP 验证后）

- **反向代理模式**：支持服务端流量采集，适合 CI/CD 和自动化测试场景
- **持续监听模式**：长期采集流量，自动发现新增/变更 API
- **文档漂移检测**：对比已有文档和最新流量，生成 diff 报告
- **跨 session 文档合并**：多次录制同一产品的不同场景，合并为完整文档
- **K8s Sidecar 部署**：零侵入注入到 Pod，自动采集
- **eBPF 内核级采集**：Linux 服务器上完全透明抓取
- **Web UI**：文档管理界面，在线编辑、版本对比、团队协作
- **多语言文档**：同一份 API 生成中英文文档
- **CI/CD 集成**：API 变更时自动触发文档更新，PR 附带 doc diff
- **多格式导入**：支持 Postman Collection、cURL 命令批量导入
