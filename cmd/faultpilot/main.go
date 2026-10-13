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

	"github.com/seronz/fault-pilot/lab/faultinjector"
)

const defaultFaultInjectorAddr = ":8081"

func faultInjectorAddr() string {
	if addr := os.Getenv("FAULT_INJECTOR_ADDR"); addr != "" {
		return addr
	}
	return defaultFaultInjectorAddr
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	injector := faultinjector.New(logger)

	addr := faultInjectorAddr()
	srv := &http.Server{
		Addr:              addr,
		Handler:           injector.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("fault injector listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		logger.Error("server error", "err", err)
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	logger.Info("fault injector stopped")
}
