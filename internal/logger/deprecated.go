package logger

import (
	"context"
	"log/slog"
	"os"
)

// Everything in this file is scaffolding for one migration and is deleted at
// the end of it. Do not add call sites.
//
// The target shape is in logger.go: a logger is constructed once and passed to
// whatever logs, and a context carries request data rather than a logger. What
// remains here are the two ways this codebase used to reach for a logger
// instead:
//
//   - the package-level L, and the Debug/Info/Warn/Error functions over it
//   - FromContext, which read a logger out of a context
//
// They stay only so that the tree compiles while call sites move over, which
// is also what lets each batch be built and tested on its own. When the last
// one is gone, so is this file.
//
// Two things about them are worth knowing while they are still here.
//
// The functions below no longer log the message as an attribute. They used to
// pass a fixed "global log" as msg and put the caller's message in a "message"
// attribute, which made every record produced through them look like the same
// event to anything that groups or filters by msg. That is fixed here rather
// than at the end of the migration, because it is worth having immediately and
// does not depend on where the logger comes from.
//
// They cannot report correlation. They log with context.Background(), so the
// handler has no request id and no span to read — the reason the migration
// exists rather than a defect in it.
var L = slog.New(correlationHandler{inner: slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})})

// FromContext returns the package-level logger.
//
// Deprecated: take a *slog.Logger as a dependency. This never read a logger
// out of ctx in practice — nothing ever put one there — so every caller was
// already getting the global.
func FromContext(context.Context) *slog.Logger { return L }

// Deprecated: use a *slog.Logger and its Context variants.
//
//nolint:sloglint // forwards the caller's message; removed with this file
func Debug(msg string, args ...any) { L.Debug(msg, args...) }

// Deprecated: use a *slog.Logger and its Context variants.
//
//nolint:sloglint // forwards the caller's message; removed with this file
func Info(msg string, args ...any) { L.Info(msg, args...) }

// Deprecated: use a *slog.Logger and its Context variants.
//
//nolint:sloglint // forwards the caller's message; removed with this file
func Warn(msg string, args ...any) { L.Warn(msg, args...) }

// Deprecated: use a *slog.Logger and its Context variants.
//
//nolint:sloglint // forwards the caller's message; removed with this file
func Error(msg string, args ...any) { L.Error(msg, args...) }
