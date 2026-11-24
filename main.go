// Package main provides the entry point for Kiro API Proxy.
//
// Kiro API Proxy is a reverse proxy service that translates Kiro API requests
// into OpenAI and Anthropic (Claude) compatible formats. Key features include:
//   - Multi-account pool with round-robin load balancing
//   - Automatic OAuth token refresh
//   - Streaming response support for real-time AI interactions
//   - Admin panel for account and configuration management
//
// The service exposes the following endpoints:
//   - /v1/messages - Claude API compatible endpoint
//   - /v1/chat/completions - OpenAI API compatible endpoint
//   - /admin - Web-based administration panel
package main

import (
	"context"
	"errors"
	"fmt"
	"kiro-go/config"
	"kiro-go/logger"
	"kiro-go/pool"
	"kiro-go/proxy"
	"kiro-go/storage"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	// 配置文件路径，支持环境变量覆盖
	configPath := "data/config.json"
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		configPath = envPath
	}

	// Encryption at rest: an invalid KIRO_ENCRYPTION_KEY is fatal, so nothing
	// is ever written in plaintext by mistake.
	if err := config.InitEncryptionFromEnv(); err != nil {
		log.Fatalf("%v", err)
	}

	// Subcommands: `kiro-go migrate` / `kiro-go export` (see cli.go).
	if len(os.Args) > 1 {
		os.Exit(runCommand(os.Args[1], os.Args[2:], configPath))
	}

	// 确保数据目录存在
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Storage backend: PostgreSQL when DATABASE_URL is set, else config.json.
	db := openDatabase(configPath)
	if db != nil {
		defer db.Close()
	}

	// 加载配置
	if err := config.Init(configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if config.EncryptionEnabled() {
		logger.Infof("Credentials are encrypted at rest (%s)", config.EncryptionKeyEnv)
	}

	// Initialize log level: LOG_LEVEL env var takes priority over config, defaulting to "info".
	logger.Init(config.GetLogLevel())

	// 环境变量覆盖密码
	if envPassword := os.Getenv("ADMIN_PASSWORD"); envPassword != "" {
		config.SetPassword(envPassword)
	}
	// Never run with the shipped default password: replace it with a random
	// one, persist it, and print it once.
	if generated, changed, err := config.EnsureAdminPassword(); err != nil {
		logger.Fatalf("Failed to set admin password: %v", err)
	} else if changed {
		logger.Warnf("Admin password was unset or the insecure default; generated a new one: %s", generated)
		logger.Warnf("It is saved in %s. Set ADMIN_PASSWORD or change it in the admin panel.", config.CurrentStore().Name())
	}

	if !config.IsApiKeyRequired() && !isLoopbackHost(config.GetHost()) {
		logger.Warnf("API key verification is OFF and the server listens on %s: anyone who can reach this port can use your Kiro accounts. Enable it under Settings → API Settings.", config.GetHost())
	}

	// 初始化账号池
	pool.GetPool()

	// 创建 HTTP 处理器（包含后台刷新任务）
	handler := proxy.NewHandler()
	var recorder *storage.Recorder
	if db != nil {
		recorder = storage.NewRecorder(db, func() time.Duration {
			days := config.GetLogRetentionDays()
			if days <= 0 {
				days = 30
			}
			return time.Duration(days) * 24 * time.Hour
		})
		handler.UseTelemetry(recorder, recorder, storage.NewResponseStore(db))
		if sessions, err := storage.NewSessionStore(context.Background(), db); err != nil {
			logger.Warnf("Admin sessions: not persisted (%v); sign-ins last until restart", err)
		} else {
			proxy.UseAdminSessionStore(sessions)
		}
		handler.UseMigrator(&storage.JSONMigrator{
			DB:          db,
			ConfigPath:  configPath,
			AutoImport:  autoImportEnabled(),
			ResponseTTL: proxy.StoredResponseTTL(),
			Reload:      reloadConfig,
		})
		logger.Infof("Storage: PostgreSQL (request logging and audit enabled)")
	} else {
		logger.Infof("Storage: %s (set DATABASE_URL to use PostgreSQL with request history)", config.CurrentStore().Name())
	}

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", config.GetHost(), config.GetPort())
	logger.Infof("Kiro-Go starting on http://%s (log level: %s)", addr, logger.LevelName(logger.GetLevel()))
	logger.Infof("Admin panel: http://%s/admin", addr)
	logger.Infof("Claude API: http://%s/v1/messages", addr)
	logger.Infof("OpenAI API: http://%s/v1/chat/completions", addr)

	// WriteTimeout intentionally 0: SSE streams can run for minutes while the
	// upstream model produces tokens. ReadHeaderTimeout + ReadTimeout still
	// guard against slowloris-style header/body stalls.
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown: finish in-flight requests, then flush pending stats
	// and queued log records before exiting.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		logger.Infof("Shutting down…")
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("Server failed: %v", err)
	}
	config.Flush()
	if recorder != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		recorder.Close(ctx)
		cancel()
	}
	logger.Infof("Stopped")
}

// isLoopbackHost reports whether the bind address only accepts local clients.
func isLoopbackHost(host string) bool {
	switch strings.TrimSpace(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// reloadConfig re-reads the configuration from the active store after it was
// replaced wholesale (console migration), re-applying environment overrides.
func reloadConfig() error {
	if err := config.Load(); err != nil {
		return err
	}
	if envPassword := os.Getenv("ADMIN_PASSWORD"); envPassword != "" {
		config.SetPassword(envPassword)
	}
	if _, _, err := config.EnsureAdminPassword(); err != nil {
		return err
	}
	pool.GetPool().Reload()
	return nil
}
