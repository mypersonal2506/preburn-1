package pricing

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/database/databasetest"
)

const (
	booleanKindName = "boolean"
	stringKindName  = "string"
	integerKindName = "integer"
)

type vocabularyEntry struct {
	Kind   string
	Values []string
}

func TestVocabulariesMatchAcrossPackagesAndMigration(t *testing.T) {
	seededMeters := migrationSeededMeters(t)

	if diff := cmp.Diff(seededMeters, pricingMeters(t)); diff != "" {
		t.Errorf("pricing meter vocabulary differs from the meters migration 0003 seeds (-seeded +pricing):\n%s", diff)
	}
	if diff := cmp.Diff(seededMeters, sortedCopy(catalogfiles.Meters())); diff != "" {
		t.Errorf("catalogfiles meters differ from the meters migration 0003 seeds (-seeded +catalogfiles):\n%s", diff)
	}
	if diff := cmp.Diff(pricingAttributeVocabulary(), catalogFilesAttributeVocabulary(), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("catalogfiles attribute vocabulary differs from pricing (-pricing +catalogfiles):\n%s", diff)
	}
}

func pricingMeters(t *testing.T) []string {
	t.Helper()
	meters := make([]string, 0, len(meterVocabulary))
	for _, meter := range meterVocabulary {
		if _, err := ParseMeter(string(meter)); err != nil {
			t.Errorf("ParseMeter(%q) error = %v, want every meter of the vocabulary accepted", meter, err)
		}
		meters = append(meters, string(meter))
	}
	return sortedCopy(meters)
}

func pricingAttributeVocabulary() map[string]vocabularyEntry {
	kindNames := map[attributeKind]string{
		attributeKindString:  stringKindName,
		attributeKindBoolean: booleanKindName,
		attributeKindInteger: integerKindName,
	}
	vocabulary := make(map[string]vocabularyEntry, len(attributeVocabulary))
	for _, requirement := range attributeVocabulary {
		vocabulary[requirement.key] = vocabularyEntry{Kind: kindNames[requirement.kind], Values: requirement.values}
	}
	return vocabulary
}

func catalogFilesAttributeVocabulary() map[string]vocabularyEntry {
	vocabulary := map[string]vocabularyEntry{}
	for _, attribute := range catalogfiles.AttributeVocabulary() {
		entry := vocabularyEntry{Kind: stringKindName, Values: attribute.Values}
		if attribute.Boolean {
			entry.Kind = booleanKindName
		}
		vocabulary[attribute.Key] = entry
	}
	return vocabulary
}

func migrationSeededMeters(t *testing.T) []string {
	t.Helper()
	pool := databasetest.NewPool(t)
	rows, err := pool.Query(t.Context(), `SELECT meter FROM meters ORDER BY meter COLLATE "C"`)
	if err != nil {
		t.Fatalf("select meters: %v", err)
	}
	meters, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read meters: %v", err)
	}
	return meters
}

func sortedCopy(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted
}
