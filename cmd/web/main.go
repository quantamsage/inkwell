// Command web runs the Markdown editor web application.
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

	"markdown-editor/internal/config"
	"markdown-editor/internal/database"
	"markdown-editor/internal/documents"
	"markdown-editor/internal/handlers"
	"markdown-editor/internal/markdown"
	"markdown-editor/internal/preferences"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	logger.Info("database ready", "path", cfg.DatabasePath)

	srv, err := handlers.New(handlers.Options{
		Documents:     documents.NewService(database.NewDocumentStore(db), cfg.MaxDocumentBytes),
		Preferences:   preferences.NewService(database.NewPreferenceStore(db)),
		Renderer:      markdown.New(),
		Logger:        logger,
		SecureCookies: cfg.SecureCookies,
	})
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "url", "http://"+displayAddr(cfg.Addr))
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
}

func displayAddr(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "localhost" + addr
	}
	return addr
}
