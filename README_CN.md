# Kiro-Go

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat&logo=docker)](docker-compose.yml)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat&logo=postgresql&logoColor=white)](docs/configuration.md#storage)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**自托管网关：把一组 Kiro 账号变成兼容 Anthropic 与 OpenAI 的 API** —— 专为在 Kiro 上运行 Claude Code、Codex、
IDE 智能体和各类 SDK 而设计，附带管理控制台、请求历史以及对上游的克制访问。

[English](README.md) | [中文](README_CN.md) | [Tiếng Việt](README_VI.md)

> Kiro-Go 最初基于 [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go)（MIT），现已重写为独立项目：
> 全新控制台、PostgreSQL 存储、按请求决定的思考模式、API 兼容性改进，以及与官方 Kiro 客户端一致的上游行为。
> 详见 [CHANGELOG.md](CHANGELOG.md)。

## 亮点

**你的工具已经会用的 API**
- Anthropic Messages（`/v1/messages`、`count_tokens`）、OpenAI Chat Completions、OpenAI Responses
  （`/v1/responses`，支持存储的响应与 `previous_response_id`）以及 `/v1/models`。
- 官方错误码（Claude Code 遇到 “prompt is too long” 会自动压缩上下文，Codex 收到 `context_length_exceeded`）、
  SSE 心跳、`tool_choice`、停止序列、请求 ID、浏览器 SDK 所需的 CORS。
- **思考模式跟随请求** —— Anthropic `thinking` / `output_config.effort`、OpenAI `reasoning_effort`、
  Responses `reasoning`；对支持的模型原生转发 effort。旧的 `-thinking` 模型后缀仍然可用。
- 通过 Kiro 的 MCP 端点转发 `web_search` 工具。

**稳健的账号池**
- 支持 Builder ID、IAM Identity Center / 企业 SSO、Microsoft SSO、SSO Token、凭证 JSON 与 Kiro API Key，自动刷新令牌。
- 会话亲和：同一会话固定在同一账号，保持 Kiro 的提示缓存命中。
- 每个请求的上游调用预算、遵循 Kiro 的 `retry-after` 提示、不重放客户端错误、限流或额度耗尽的账号自动冷却、可选的单账号并发上限。
- 按账号自动选择上游协议（`/v1/responses` 或 `generateAssistantResponse`），请求格式与请求头和官方 Kiro CLI 对齐。

**好用的控制台**（`/admin`）
- 概览（流量与用量分布）、账号（额度与健康状态）、模型目录（可启用/禁用）、Playground、API Key（按 Key 配额、一键复制客户端配置）。
- PostgreSQL 中的完整请求历史与管理审计日志，可选记录请求/响应正文 —— 在请求路径之外异步写入，不影响推理速度。
- English、中文、Tiếng Việt；浅色/深色主题；适配手机。

## 快速开始

### Docker Compose（推荐）

```bash
git clone https://github.com/springmusk026/Kiro-Go.git
cd Kiro-Go
docker compose up -d
docker compose logs kiro-go | grep -i password   # 首次启动会打印自动生成的管理员密码
```

这会在 `8080` 端口启动 Kiro-Go，并附带一个 PostgreSQL 16 容器。打开 `http://localhost:8080/admin` 登录，添加账号并创建
API Key。可在 `.env` 中设置 `ADMIN_PASSWORD`（以及 `POSTGRES_PASSWORD`）使用自己的密码。

之后更新：

```bash
git pull && docker compose up -d --build
```

从 `config.json` 部署迁移过来？首次启动会自动导入，控制台会引导你校验并删除该文件 ——
见[迁移说明](docs/configuration.md#migrating-from-configjson)。

### Docker 镜像

多架构镜像（amd64、arm64）发布在 `ghcr.io/springmusk026/kiro-go` —— `main` 分支对应 `latest`，另有各版本标签：

```bash
docker run -d --name kiro-go -p 8080:8080 \
  -e ADMIN_PASSWORD=change-me \
  -e DATABASE_URL="postgres://kiro:kiro@db-host:5432/kiro?sslmode=disable" \
  -v "$PWD/data:/app/data" \
  --restart unless-stopped ghcr.io/springmusk026/kiro-go:latest
```

不设置 `DATABASE_URL` 时，所有数据保存在 `data/config.json`（没有请求历史）。

### 从源码构建

需要 Go 1.25+ 与 Node 22+（用于控制台）。

```bash
git clone https://github.com/springmusk026/Kiro-Go.git && cd Kiro-Go
(cd frontend && npm ci && npm run build)   # 构建控制台到 ./web
go build -o kiro-go .
./kiro-go
```

## 接入客户端

**API & Keys** 页面会按你的服务器地址、Key 和模型生成以下配置。

**Claude Code**

```bash
export ANTHROPIC_BASE_URL=http://localhost:8080
export ANTHROPIC_AUTH_TOKEN=<your-api-key>
export ANTHROPIC_MODEL=claude-sonnet-4.5
export ANTHROPIC_SMALL_FAST_MODEL=claude-haiku-4.5
claude
```

**Codex CLI**（`~/.codex/config.toml`）

```toml
model = "claude-sonnet-4.5"
model_provider = "kiro"
model_reasoning_effort = "medium"

[model_providers.kiro]
name = "Kiro-Go"
base_url = "http://localhost:8080/v1"
env_key = "KIRO_API_KEY"
wire_api = "responses"
```

**OpenAI / Anthropic SDK** —— 将 `base_url` 指向 `http://localhost:8080/v1`（OpenAI）或 `http://localhost:8080`（Anthropic），
并使用 Kiro-Go 的 API Key。

更多客户端（Cline、Continue、LiteLLM、cURL）与思考模式选项见 [docs/clients.md](docs/clients.md)。

## 文档（英文）

| | |
|---|---|
| [docs/clients.md](docs/clients.md) | Claude Code、Codex、SDK 及其他工具的接入 |
| [docs/api.md](docs/api.md) | 接口、兼容性说明、思考模式、模型、错误码 |
| [docs/configuration.md](docs/configuration.md) | 环境变量、设置项、存储、从 JSON 迁移、日志 |
| [docs/architecture.md](docs/architecture.md) | 请求流程、上游协议、账号池、缓存与成本 |
| [docs/development.md](docs/development.md) | 构建、测试、控制台开发、发布 |
| [CHANGELOG.md](CHANGELOG.md) | 变更记录，包括与原项目的差异 |

## 安全

- 服务默认监听 `0.0.0.0`。对外暴露前请**创建 API Key**（或在设置中开启 API Key 校验），否则任何能访问端口的人都能使用你的账号；未开启时控制台会给出警告。
- 未设置时首次启动会生成管理员密码；管理登录有频率限制并使用常量时间比较；控制台下发严格的安全响应头。
- 请求/响应正文记录默认关闭，因为提示词可能包含敏感信息。
- 对外提供服务时请在前面加 TLS（Caddy、nginx、Cloudflare）。

## 免责声明

仅供学习与研究使用。与 Amazon、AWS、Kiro 无关。使用者需自行遵守所用账号的服务条款及相关法律。

## 许可证

[MIT](LICENSE)。部分代码源自 [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go)（同为 MIT）。
