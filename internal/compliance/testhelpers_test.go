package compliance

import (
	"log/slog"
	"io"
)

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
