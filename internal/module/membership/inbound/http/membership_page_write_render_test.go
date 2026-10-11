package membershiphttp

import (
	"strings"
	"testing"
)

func TestMembershipTemplatesDeclareExistingWriteAPIs(t *testing.T) {
	tier := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Edit": &membershipTierEdit{ID: 2, Name: "Silver", ThresholdAmount: 100005, SortOrder: -1},
	}))
	assign := renderMembershipPage(t, "admin/membership/membership_assignments.html", assignmentsPageFixture(nil))
	for _, tc := range []struct {
		name, body string
		apis       []string
		count      int
	}{
		{"tiers", tier, []string{"/api/membership/tier/create", "/api/membership/tier/update", "/api/membership/tier/delete", "/api/membership/entitlement/save"}, 5},
		{"assignments", assign, []string{"/api/membership/assign/set", "/api/membership/assign/unlock"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, api := range tc.apis {
				if !strings.Contains(tc.body, `data-write-api="`+api+`"`) {
					t.Errorf("missing API %s", api)
				}
			}
			if strings.Count(tc.body, "data-write-api=") != tc.count || strings.Count(tc.body, "data-write-status") != tc.count {
				t.Error("every write requires one API declaration and one local result slot")
			}
			if strings.Contains(tc.body, `method="post"`) || strings.Contains(tc.body, `action="/admin/membership/tier/`) || strings.Contains(tc.body, `action="/admin/membership/assign/`) {
				t.Error("retired page POST revived")
			}
			if !strings.Contains(tc.body, "<noscript>") || !strings.Contains(tc.body, `data-requires-js hidden disabled`) || !strings.Contains(tc.body, `name="csrf-token" content="test-token"`) {
				t.Error("JS-only writes must retain CSRF and explicit no-JS explanation")
			}
		})
	}
	for _, want := range []string{`data-write-builder="membership-tier"`, `data-write-builder="membership-entitlements"`, `data-membership-create="true"`, `data-membership-shipping-kind="free_shipping"`, `data-membership-discount-kind="discount"`, `<script src="/static/js/ui/membership_write.js"></script>`, `name="thresholdYuan" value="1000.05"`, `name="tierId" value="2"`} {
		if !strings.Contains(tier, want) {
			t.Errorf("tier form contract missing %s", want)
		}
	}
}
