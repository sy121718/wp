package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
)

func findSeedByVersion(t *testing.T, version string) migrations.Seed {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == version {
			return seed
		}
	}
	t.Fatalf("种子 %s 未注册；本用例的终值由它收口", version)
	return migrations.Seed{}
}
