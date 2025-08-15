package logging

import (
	"log/slog"
	"os"
)

type Config struct {
	Env       string       // "Development" | "Production" | etc.
	Level     slog.Leveler // slog.LevelInfo, slog.LevelDebug, ...
	AddSource bool
}

func New(cfg Config) *slog.Logger {
	var h slog.Handler
	if cfg.Env == "Development" {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:     cfg.Level,
			AddSource: cfg.AddSource,
		})
	} else { // Production
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     cfg.Level,
			AddSource: cfg.AddSource,
			// ReplaceAttr can scrub PII or rename fields if needed
			// ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr { return a },
		})
	}
	return slog.New(h)
}
