package config_test

import (
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/preburn/preburn/internal/config"
)

const (
	configurationDocumentPath = "../../docs/configuration.md"
	environmentExamplePath    = "../../.env.example"
)

var variablesComposeSets = []string{
	"PREBURN_DATABASE_URL",
	"PREBURN_REDIS_URL",
	"PREBURN_HTTP_ADDRESS",
	"PREBURN_METRICS_ADDRESS",
	"PREBURN_STRIPE_API_BASE",
}

func TestConfigurationDocumentListsEveryVariable(t *testing.T) {
	document, err := os.ReadFile(configurationDocumentPath)
	if err != nil {
		t.Fatalf("read %s: %v", configurationDocumentPath, err)
	}
	for _, name := range loadedVariableNames() {
		if !regexp.MustCompile("(?m)^\\| `" + name + "` \\|").Match(document) {
			t.Errorf("docs/configuration.md has no table row for %s", name)
		}
	}
}

func TestEnvironmentExampleListsEveryVariableUsersSet(t *testing.T) {
	example, err := os.ReadFile(environmentExamplePath)
	if err != nil {
		t.Fatalf("read %s: %v", environmentExamplePath, err)
	}
	names := loadedVariableNames()
	for _, name := range variablesComposeSets {
		if !slices.Contains(names, name) {
			t.Errorf("variablesComposeSets names %s, which config.Load does not read", name)
		}
	}
	for _, name := range names {
		if slices.Contains(variablesComposeSets, name) {
			continue
		}
		if !regexp.MustCompile("(?m)^" + name + "=").Match(example) {
			t.Errorf(".env.example does not set %s", name)
		}
	}
}

func loadedVariableNames() []string {
	var names []string
	_, _ = config.Load(func(name string) (string, bool) {
		names = append(names, name)
		return "", false
	})
	return names
}
