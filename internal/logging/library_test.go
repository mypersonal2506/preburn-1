package logging_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/logging"
)

func TestLibraryLoggerDropsRecordsBelowWarn(t *testing.T) {
	var output bytes.Buffer
	library := logging.New(&output, slog.LevelDebug).Library("river")

	library.Log(t.Context(), slog.LevelInfo, "River client started")

	if output.Len() != 0 {
		t.Errorf("output = %q, want nothing", output.String())
	}
}

func TestLibraryLoggerWritesWarningsAsLibraryMessageEvents(t *testing.T) {
	var output bytes.Buffer
	library := logging.New(&output, slog.LevelDebug).Library("river")

	library.Log(t.Context(), slog.LevelWarn, "job failed", slog.Int("attempt", 3), slog.String("secret", secretValue))

	record := decodeRecord(t, output.Bytes())
	delete(record, slog.TimeKey)
	want := map[string]any{
		"level":   "WARN",
		"msg":     string(logging.LoggingLibraryMessage),
		"library": "river",
		"message": "job failed",
		"attempt": float64(3),
		"secret":  "[redacted]",
	}
	if diff := cmp.Diff(want, record); diff != "" {
		t.Errorf("record mismatch (-want +got):\n%s", diff)
	}
}
