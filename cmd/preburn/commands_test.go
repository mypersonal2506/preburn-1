package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/logging"
)

const (
	committedOpenAPIPath = "../../api/openapi.json"
	secretKeyLength      = 32
)

func TestOpenAPIMatchesCommittedDocument(t *testing.T) {
	committed, err := os.ReadFile(committedOpenAPIPath)
	if err != nil {
		t.Fatalf("read committed document: %v", err)
	}
	var output bytes.Buffer

	if err := execute(t.Context(), &output, "openapi"); err != nil {
		t.Fatalf("openapi: %v", err)
	}

	if diff := cmp.Diff(string(committed), output.String()); diff != "" {
		t.Errorf("openapi output differs from %s, run make generate-openapi (-committed +generated):\n%s", committedOpenAPIPath, diff)
	}
}

func TestMigrateTwiceOnEmptyDatabase(t *testing.T) {
	setEnvironment(t, databasetest.NewEmptyPool(t).Config().ConnString())

	for run := 1; run <= 2; run++ {
		output := &lockedBuffer{}
		if code := exitCode(execute(t.Context(), output, "migrate")); code != exitSuccess {
			t.Fatalf("run %d exit code = %d, want %d", run, code, exitSuccess)
		}
		if records := output.records(t, logging.DatabaseMigrationsApplied); len(records) != 1 {
			t.Errorf("run %d logged database.migrations_applied %d times, want 1", run, len(records))
		}
	}
}

func TestSecretKeyPrintsNewKey(t *testing.T) {
	var output bytes.Buffer

	if err := execute(t.Context(), &output, "secret-key"); err != nil {
		t.Fatalf("secret-key: %v", err)
	}

	key, hasNewline := strings.CutSuffix(output.String(), "\n")
	if !hasNewline {
		t.Errorf("output %q does not end with a newline", output.String())
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != secretKeyLength {
		t.Errorf("key decodes to %d bytes, err %v, want %d bytes of standard base64", len(decoded), err, secretKeyLength)
	}
}
