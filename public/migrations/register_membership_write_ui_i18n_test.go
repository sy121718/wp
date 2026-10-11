package migrations

import (
	"strings"
	"testing"
)

func TestMembershipWriteUISeedHasExplicitLanguagePairs(t *testing.T) {
	var seed *Seed
	for i := range allSeeds {
		if allSeeds[i].Version == "606-membership-write-ui-i18n" {
			seed = &allSeeds[i]
			break
		}
	}
	if seed == nil {
		t.Fatal("membership write seed missing")
	}
	for _, want := range []string{"COUNT(*) = 2", "'admin.membership.write.noJs'", "'zh-CN'", "'en-US'"} {
		if !strings.Contains(seed.ConditionSQL, want) {
			t.Errorf("missing explicit guard %q: %s", want, seed.ConditionSQL)
		}
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		if strings.Count(seed.SQL, "('admin.membership.write.noJs', '"+lang+"'") != 1 {
			t.Errorf("expected one membership key for %s", lang)
		}
	}
	if strings.Contains(seed.ConditionSQL, "LIKE") || !strings.Contains(seed.SQL, "ON CONFLICT (item_key, lang) DO NOTHING") {
		t.Fatal("membership seed must backfill explicit missing pairs")
	}
}
