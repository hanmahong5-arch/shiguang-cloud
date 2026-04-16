// Package sglog provides structured logging initialization for all
// ShiguangSuite binaries using Go's log/slog.
//
// Usage (in each binary's main.go):
//
//	sglog.Init("hub", false)  // production: JSON output
//	sglog.Init("hub", true)   // development: text output with source
//
// After Init(), both slog.Info/Warn/Error and stdlib log.Printf
// output through the same structured handler. Existing log.Printf
// calls are automatically bridged — no need to rewrite everything at once.
//
// Structured logging convention:
//
//	slog.Info("server started", "bind", addr, "tls", cfg.TLS != nil)
//	slog.Error("database connection failed", "err", err, "dsn", dsn[:40])
//	slog.Warn("rate limit hit", "ip", ip, "endpoint", path)
package sglog

import (
	"io"
	"log"
	"log/slog"
	"os"
)

// Init configures the global slog default logger and bridges stdlib log.
//
// Parameters:
//   - service: binary name used as the "service" attribute in every log line
//   - dev: if true, use human-readable text output; if false, use JSON
//
// After calling Init, all slog.Info/Warn/Error calls and all stdlib
// log.Printf calls are routed through the same handler.
func Init(service string, dev bool) {
	var handler slog.Handler

	if dev {
		// Development: human-readable text, include source location
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		})
	} else {
		// Production: JSON for log aggregation (ELK/Loki/CloudWatch)
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}

	// Add service name as a default attribute to every log line
	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)

	// Bridge stdlib log.Printf → slog at Info level.
	// This makes existing log.Printf("[hub] msg") calls appear as
	// structured log entries without requiring code changes.
	slog.SetLogLoggerLevel(slog.LevelInfo)

	// Remove stdlib log's own timestamp/prefix — slog adds its own.
	log.SetFlags(0)
	log.SetOutput(slogWriter{})
}

// InitWriter is like Init but directs output to a custom writer
// (useful for testing or log file rotation).
func InitWriter(service string, w io.Writer) {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)
	slog.SetLogLoggerLevel(slog.LevelInfo)
	log.SetFlags(0)
	log.SetOutput(slogWriter{})
}

// slogWriter implements io.Writer for bridging stdlib log → slog.
// It routes log.Printf output through slog's default logger.
type slogWriter struct{}

func (slogWriter) Write(p []byte) (n int, err error) {
	// Strip trailing newline that log.Printf adds
	msg := string(p)
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	slog.Info(msg)
	return len(p), nil
}
