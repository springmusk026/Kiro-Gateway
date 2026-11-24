# Admin console: build the static SPA from frontend/ (served under /admin).
FROM --platform=$BUILDPLATFORM node:22-alpine AS console
WORKDIR /console
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build:only

# builder 阶段始终运行在构建机原生平台（amd64），用 Go 交叉编译目标平台二进制
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o kiro-go .

FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/kiro-go .
COPY --from=console /console/dist/client ./web
RUN mkdir -p /app/data

EXPOSE 8080
VOLUME /app/data

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1

CMD ["./kiro-go"]
