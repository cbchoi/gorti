// Command rtid runs the gorti RTI server.
//
// This is a skeleton — Agent A wires up the gRPC services and core
// implementations during M2/M3.
package main

import (
	"flag"
	"log/slog"
	"os"
)

func main() {
	listen := flag.String("listen", ":8442", "gRPC listen address")
	metricsListen := flag.String("metrics-listen", ":9090", "Prometheus HTTP listen")
	logLevel := flag.String("log-level", "info", "log level: debug|info|warn|error")
	logFormat := flag.String("log-format", "json", "log format: json|text")
	flag.Parse()

	level := slog.LevelInfo
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if *logFormat == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	logger.Info("rtid starting (skeleton — services not wired)",
		"listen", *listen,
		"metrics_listen", *metricsListen,
	)

	// TODO(#1): wire FederationService, DeclarationService, ObjectService,
	// TimeService, StreamService against rti/internal/* implementations.
	// Owner: Agent A, Milestone: M2.
	logger.Error("rtid is not yet implemented; blocked on M2 deliverables")
	os.Exit(1)
}
