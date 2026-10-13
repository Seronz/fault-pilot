package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/seronz/fault-pilot/internal/approval"
	"github.com/seronz/fault-pilot/internal/collector"
	"github.com/seronz/fault-pilot/internal/config"
	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/executor"
	"github.com/seronz/fault-pilot/internal/observability"
	"github.com/seronz/fault-pilot/internal/policy"
	"github.com/seronz/fault-pilot/internal/store"
	"github.com/seronz/fault-pilot/lab/faultinjector"
)

const defaultFaultInjectorAddr = ":8081"

// faultInjectorAddr mengembalikan alamat listen fault injector.
func faultInjectorAddr() string {
	if addr := os.Getenv("FAULT_INJECTOR_ADDR"); addr != "" {
		return addr
	}
	return defaultFaultInjectorAddr
}

// loadConfig memuat config.yaml (path bisa dioverride lewat FAULTPILOT_CONFIG).
func loadConfig(logger *slog.Logger) (config.Config, error) {
	path := os.Getenv("FAULTPILOT_CONFIG")
	if path == "" {
		path = "config.yaml"
	}
	cfg, err := config.Load(path)
	if err != nil {
		logger.Error("gagal memuat config", "path", path, "err", err)
		return config.Config{}, err
	}
	return cfg, nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := loadConfig(logger)
	if err != nil {
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Store SQLite (juga dipakai health check + policy). Bila gagal, health
	// check sqlite dilewati (fail closed untuk aksi, tidak mematikan observability).
	var st *store.Store
	if s, err := store.Open(ctx, cfg.Observability.StorePath); err != nil {
		logger.Warn("store tidak tersedia, health check sqlite dilewati", "err", err)
	} else {
		st = s
		defer st.Close()
	}

	// Collector menerima event lalu menuliskannya ke store.
	processor := func(context.Context, domain.Event) error { return nil }
	if st != nil {
		processor = func(pctx context.Context, e domain.Event) error { return st.AppendEvent(pctx, e) }
	}
	col := collector.New(collector.DefaultBufferSize, collector.DefaultMaxConcurrent, processor, logger)
	defer func() { _ = col.Close() }()

	// Metrics (kedalaman antrean diambil dari collector saat scrape).
	metrics := observability.NewMetrics(observability.WithQueueDepthFunc(col.Len))

	// RBAC + Human Approval Gate. Kegagalan memuat RBAC => semua akses ditolak
	// (fail closed), bukan mematikan proses.
	rbac, rerr := approval.LoadRBAC(cfg.Approval.RBACPath)
	if rerr != nil {
		logger.Warn("rbac tidak valid, semua approval ditolak", "path", cfg.Approval.RBACPath, "err", rerr)
		rbac, _ = approval.NewRBAC(approval.RBACConfig{})
	}
	ttl, terr := time.ParseDuration(cfg.Approval.TTL)
	if terr != nil || ttl <= 0 {
		ttl = approval.DefaultTTL
	}
	gate := approval.NewGate(rbac, approval.WithTTL(ttl), approval.WithObserver(metrics))

	// Policy engine: allowlist + Human Approval Gate. Aksi berisiko tinggi wajib
	// disetujui manusia sebelum executor menjalankannya.
	var policyStore policy.Store
	if st != nil {
		policyStore = st
	}
	requireRisks := cfg.Approval.RequireRisks
	var approvalGate policy.ApprovalGate
	if cfg.Approval.Enabled {
		approvalGate = gate
	} else {
		requireRisks = []string{}
	}
	risk := executor.RiskForType(cfg.Executor.Type)
	engine := policy.NewWithConfig(policyStore, policy.Config{
		AllowedTargets:       cfg.Executor.Allowlist.Targets,
		RiskLevels:           map[string]string{policy.ActionRestartSimulatedService: risk},
		RequireApprovalRisks: requireRisks,
		Approval:             approvalGate,
		Observer:             metrics,
	})

	exec, err := executor.Build(executor.BuildConfig{
		Type:            cfg.Executor.Type,
		AllowedTargets:  cfg.Executor.Allowlist.Targets,
		AllowedRunbooks: cfg.Executor.Allowlist.Runbooks,
		FIURL:           cfg.Executor.FaultInjectorURL,
		Policy:          engine,
	})
	if err != nil {
		logger.Error("gagal membangun executor", "err", err)
		os.Exit(1)
	}
	logger.Info("executor configured", "type", exec.Type(), "risk", risk, "approval", cfg.Approval.Enabled)
	// Bungkus executor agar setiap eksekusi tercatat (actions_total + duration).
	_ = observability.InstrumentExecutor(exec, metrics)

	// Heartbeat "poll terakhir" (nanti diperbarui oleh connector poller).
	heartbeat := &observability.Heartbeat{}
	heartbeat.Mark()
	maxPoll, perr := time.ParseDuration(cfg.Observability.LastPollMax)
	if perr != nil || maxPoll <= 0 {
		maxPoll = 2 * time.Minute
	}

	// Health checker: deep check sqlite + worker + last poll.
	checker := observability.NewChecker()
	if st != nil {
		checker.AddCheck("sqlite", st.Ping)
	}
	checker.AddCheck("worker", col.Healthy)
	checker.AddCheck("last_poll", heartbeat.Probe(maxPoll))

	// Self-monitoring: log WARN "self-degraded" bila antrean/error melewati ambang.
	monitor := observability.NewSelfMonitor(logger, metrics, col.Len)
	go monitor.Run(ctx)

	// Server observability: /metrics, /healthz, /readyz.
	obsSrv := &http.Server{
		Addr:              cfg.Observability.MetricsAddr,
		Handler:           observability.NewServer(metrics.Registry(), checker).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("observability listening", "addr", cfg.Observability.MetricsAddr)
		if err := obsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("observability server error", "err", err)
		}
	}()

	// Server Human Approval Gate: /approvals (GET/POST, approve/reject).
	errCh := make(chan error, 1)
	var approvalSrv *http.Server
	if cfg.Approval.Enabled {
		approvalSrv = &http.Server{
			Addr:              cfg.Approval.Addr,
			Handler:           approval.NewHandler(gate).Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			logger.Info("approval listening", "addr", cfg.Approval.Addr)
			if err := approvalSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	}

	// Server fault injector (lab).
	injector := faultinjector.New(logger)
	addr := faultInjectorAddr()
	srv := &http.Server{
		Addr:              addr,
		Handler:           injector.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("fault injector listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Sistem siap menerima insiden setelah kedua server berjalan.
	checker.SetReady(true)

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		logger.Error("server error", "err", err)
		stop()
	}

	checker.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("injector shutdown failed", "err", err)
	}
	if err := obsSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("observability shutdown failed", "err", err)
	}
	if approvalSrv != nil {
		if err := approvalSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error("approval shutdown failed", "err", err)
		}
	}
	logger.Info("stopped")
}
