// Package logging provides structured JSON logging for the application,
// built on zerolog.
package logging

import (
	"io"

	"github.com/rs/zerolog"
)

// New constructs a structured JSON logger writing to w, filtered to the
// given minimum level ("debug", "info", "warn", "error"). An unrecognized
// level falls back to "info".
func New(w io.Writer, level string) zerolog.Logger {
	parsedLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		parsedLevel = zerolog.InfoLevel
	}

	return zerolog.New(w).
		Level(parsedLevel).
		With().
		Timestamp().
		Logger()
}
