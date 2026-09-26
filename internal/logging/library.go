package logging

import (
	"context"
	"log/slog"
)

const (
	libraryAttributeKey        = "library"
	libraryMessageAttributeKey = "message"
	libraryMinimumLevel        = slog.LevelWarn
)

type libraryHandler struct {
	handler slog.Handler
	library string
}

// Library returns a *slog.Logger for a third-party library that logs its own
// prose messages, such as River. It drops records below the warn level and
// writes each other record as the event logging.library_message with the
// library name, the original message and the record's attributes.
func (logger *Logger) Library(library string) *slog.Logger {
	return slog.New(libraryHandler{handler: logger.base.Handler(), library: library})
}

func (handler libraryHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= libraryMinimumLevel && handler.handler.Enabled(ctx, level)
}

func (handler libraryHandler) Handle(ctx context.Context, record slog.Record) error {
	event := slog.NewRecord(record.Time, record.Level, string(LoggingLibraryMessage), record.PC)
	event.AddAttrs(slog.String(libraryAttributeKey, handler.library), slog.String(libraryMessageAttributeKey, record.Message))
	record.Attrs(func(attribute slog.Attr) bool {
		event.AddAttrs(attribute)
		return true
	})
	return handler.handler.Handle(ctx, event)
}

func (handler libraryHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return libraryHandler{handler: handler.handler.WithAttrs(attributes), library: handler.library}
}

func (handler libraryHandler) WithGroup(name string) slog.Handler {
	return libraryHandler{handler: handler.handler.WithGroup(name), library: handler.library}
}
