package util

import (
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
)

func ConfigureLogging(jsonLogging bool) {
	if jsonLogging {
		slog.SetDefault(slog.New(
			slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				AddSource: true,
				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					// TODO: we can catch error attributes, check if the error carries metadata, and return a complex Attr (if it's even possible)
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
				TimeFormat: "15:04:05",
			}),
		))
	}
}
