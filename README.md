# apidoc - API 文档生成工具

从真实流量（HAR 或 Chrome Extension 采集）自动生成 API 文档，并提供预览服务器。

## 特性
- HAR 文件导入
- Chrome Extension 流量采集与导入
- 基于 LLM 的 API 文档生成
- 预览服务器（UI + API + 文档静态服务）

## 快速开始
1. 初始化配置与数据库
```bash
apidoc init
```

2. 导入 HAR
```bash
apidoc import --har ./example.har --scenario "登录与下单流程"
```

3. 生成文档
```bash
apidoc generate --har ./example.har --scenario "登录与下单流程"
```

4. 启动预览服务
```bash
apidoc serve --host 127.0.0.1 --port 3000
```

## Chrome Extension 使用
- 安装扩展并开始录制流量
- 录制完成后，一键发送到本地 `apidoc serve` 启动的服务
- 在预览 UI 中查看会话、日志和生成结果

## 配置参考
配置文件默认位于 `~/.apidoc/config.yaml`，常用项：
- `llm.api_key`：LLM 服务密钥
- `llm.model`：模型名称
- `output.dir`：生成文件输出目录
- `server.host` / `server.port`：预览服务监听地址
