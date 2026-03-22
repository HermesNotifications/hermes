package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ListenAndServe starts the HTTP server, blocks until SIGINT/SIGTERM,
// then gracefully shuts down. Optional onShutdown callbacks run before
// the HTTP server is stopped (e.g., flushing batches, stopping consumers).
func ListenAndServe(addr string, handler http.Handler, logger *slog.Logger, onShutdown ...func()) {
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	go func() {
		logger.Info("service starting", "addr", addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
	for _, fn := range onShutdown {
		fn()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("http shutdown error", "error", err)
	}
}
