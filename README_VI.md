# Kiro-Go

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat&logo=docker)](docker-compose.yml)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat&logo=postgresql&logoColor=white)](docs/configuration.md#storage)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**Gateway tự host biến một nhóm tài khoản Kiro thành API tương thích Anthropic và OpenAI** — dành cho việc chạy
Claude Code, Codex, các agent trong IDE và SDK trên Kiro, kèm bảng quản trị, lịch sử request và cách gọi upstream
thận trọng.

[English](README.md) | [中文](README_CN.md) | [Tiếng Việt](README_VI.md)

> Kiro-Go khởi đầu từ [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go) (MIT) và nay đã được viết lại
> thành một dự án độc lập: bảng quản trị mới, lưu trữ PostgreSQL, thinking theo từng request, cải thiện tương
> thích API và hành vi upstream giống client Kiro chính thức. Xem [CHANGELOG.md](CHANGELOG.md).

## Điểm nổi bật

**Các API mà công cụ của bạn đã dùng**
- Anthropic Messages (`/v1/messages`, `count_tokens`), OpenAI Chat Completions, OpenAI Responses
  (`/v1/responses`, có lưu response và `previous_response_id`) và `/v1/models`.
- Mã lỗi chính thức (Claude Code tự nén ngữ cảnh khi gặp "prompt is too long", Codex nhận
  `context_length_exceeded`), heartbeat SSE, `tool_choice`, stop sequence, request id, CORS cho SDK trên trình duyệt.
- **Thinking theo request** — Anthropic `thinking` / `output_config.effort`, OpenAI `reasoning_effort`,
  Responses `reasoning`; mức effort được chuyển nguyên bản tới các mô hình hỗ trợ. Hậu tố `-thinking` cũ vẫn dùng được.
- Công cụ `web_search` được chuyển qua endpoint MCP của Kiro.

**Nhóm tài khoản ổn định**
- Builder ID, IAM Identity Center / SSO doanh nghiệp, Microsoft SSO, SSO token, credential JSON và Kiro API key; tự làm mới token.
- Giữ mỗi cuộc hội thoại trên một tài khoản để cache prompt của Kiro luôn được dùng lại.
- Giới hạn số lần gọi upstream cho mỗi request, tuân theo `retry-after` của Kiro, không gửi lại lỗi phía client,
  tạm nghỉ tài khoản bị giới hạn hoặc hết hạn mức, tùy chọn giới hạn đồng thời theo tài khoản.
- Tự chọn giao thức upstream theo tài khoản (`/v1/responses` hoặc `generateAssistantResponse`), dạng request và
  header giống Kiro CLI chính thức.

**Bảng quản trị dễ dùng** (`/admin`)
- Tổng quan lưu lượng và mức dùng, tài khoản (hạn mức, tình trạng), danh mục mô hình (bật/tắt), Playground,
  API key (hạn mức theo key, đoạn cấu hình client sao chép sẵn).
- Lịch sử request đầy đủ và nhật ký kiểm tra thao tác quản trị trong PostgreSQL, tùy chọn lưu nội dung request/response —
  ghi bất đồng bộ nên không làm chậm suy luận.
- English, 中文, Tiếng Việt; giao diện sáng/tối; dùng tốt trên điện thoại.

## Bắt đầu nhanh

### Docker Compose (khuyên dùng)

```bash
git clone https://github.com/springmusk026/Kiro-Go.git
cd Kiro-Go
docker compose up -d
docker compose logs kiro-go | grep -i password   # lần chạy đầu in ra mật khẩu quản trị được tạo
```

Lệnh này chạy Kiro-Go ở cổng `8080` cùng một container PostgreSQL 16. Mở `http://localhost:8080/admin`, đăng
nhập, thêm tài khoản và tạo API key. Đặt `ADMIN_PASSWORD` (và `POSTGRES_PASSWORD`) trong file `.env` để dùng mật khẩu riêng.

Cập nhật về sau:

```bash
git pull && docker compose up -d --build
```

Đang dùng `config.json`? Dữ liệu được nhập tự động ở lần chạy đầu và bảng quản trị sẽ hướng dẫn kiểm tra rồi xóa
file — xem [di chuyển dữ liệu](docs/configuration.md#migrating-from-configjson).

### Docker image

Image đa kiến trúc (amd64, arm64) được phát hành tại `ghcr.io/springmusk026/kiro-go` — `latest` từ nhánh `main`, kèm tag theo phiên bản:

```bash
docker run -d --name kiro-go -p 8080:8080 \
  -e ADMIN_PASSWORD=change-me \
  -e DATABASE_URL="postgres://kiro:kiro@db-host:5432/kiro?sslmode=disable" \
  -v "$PWD/data:/app/data" \
  --restart unless-stopped ghcr.io/springmusk026/kiro-go:latest
```

Không có `DATABASE_URL` thì mọi thứ được lưu trong `data/config.json` (không có lịch sử request).

### Build từ mã nguồn

Cần Go 1.25+ và Node 22+ (cho bảng quản trị).

```bash
git clone https://github.com/springmusk026/Kiro-Go.git && cd Kiro-Go
(cd frontend && npm ci && npm run build)   # build bảng quản trị vào ./web
go build -o kiro-go .
./kiro-go
```

## Kết nối client

Trang **API & Keys** tạo sẵn các đoạn cấu hình này theo máy chủ, key và mô hình của bạn.

**Claude Code**

```bash
export ANTHROPIC_BASE_URL=http://localhost:8080
export ANTHROPIC_AUTH_TOKEN=<your-api-key>
export ANTHROPIC_MODEL=claude-sonnet-4.5
export ANTHROPIC_SMALL_FAST_MODEL=claude-haiku-4.5
claude
```

**Codex CLI** (`~/.codex/config.toml`)

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

**OpenAI / Anthropic SDK** — trỏ `base_url` tới `http://localhost:8080/v1` (OpenAI) hoặc `http://localhost:8080`
(Anthropic) và dùng API key của Kiro-Go.

Thêm client khác (Cline, Continue, LiteLLM, cURL) và tùy chọn thinking: [docs/clients.md](docs/clients.md).

## Tài liệu (tiếng Anh)

| | |
|---|---|
| [docs/clients.md](docs/clients.md) | Cấu hình Claude Code, Codex, SDK và các công cụ khác |
| [docs/api.md](docs/api.md) | Endpoint, ghi chú tương thích, thinking, mô hình, mã lỗi |
| [docs/configuration.md](docs/configuration.md) | Biến môi trường, cài đặt, lưu trữ, di chuyển từ JSON, ghi log |
| [docs/architecture.md](docs/architecture.md) | Luồng request, giao thức upstream, nhóm tài khoản, cache và chi phí |
| [docs/development.md](docs/development.md) | Build, kiểm thử, phát triển bảng quản trị, phát hành |
| [CHANGELOG.md](CHANGELOG.md) | Lịch sử thay đổi, gồm cả khác biệt so với dự án gốc |

## Bảo mật

- Máy chủ lắng nghe trên `0.0.0.0`. Hãy **tạo API key** (hoặc bật xác thực API key trong Cài đặt) trước khi công khai;
  nếu không, bất kỳ ai truy cập được cổng đều dùng được tài khoản của bạn. Bảng quản trị sẽ cảnh báo khi chưa bật.
- Mật khẩu quản trị được tạo ở lần chạy đầu nếu chưa đặt; đăng nhập quản trị bị giới hạn tần suất và so sánh thời gian
  hằng; bảng quản trị gửi các header bảo mật nghiêm ngặt.
- Mặc định không lưu nội dung request/response vì prompt có thể nhạy cảm.
- Đặt TLS phía trước (Caddy, nginx, Cloudflare) khi công khai ra ngoài.

## Miễn trừ trách nhiệm

Chỉ dành cho mục đích học tập và nghiên cứu. Không liên kết với Amazon, AWS hay Kiro. Người dùng tự chịu trách
nhiệm tuân thủ điều khoản dịch vụ của tài khoản đang dùng và pháp luật liên quan.

## Giấy phép

[MIT](LICENSE). Một phần mã có nguồn gốc từ [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go) (cũng theo MIT).
