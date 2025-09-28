package util

import (
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
)

func ConfigureLogging(jsonLogging bool) {
	if jsonLogging {
		slog.SetDefault(slog.New(
			slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				AddSource: true,
				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						a.Key = "timestamp"
					} else if a.Key == slog.LevelKey {
						a.Key = "severity"
					} else if a.Key == slog.MessageKey {
						a.Key = "message"
					}
					return a
				},
			})))
	} else {
		slog.SetDefault(slog.New(
			tint.NewHandler(os.Stderr, &tint.Options{
				TimeFormat: time.Kitchen,
			}),
		))
	}
}
