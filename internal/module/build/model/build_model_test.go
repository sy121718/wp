package buildmodel

import (
	"context"
	"testing"
	"time"
)

func TestClaimRejectsNonPositiveLease(t *testing.T) {
	m := NewModel(nil)
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := m.Claim(context.Background(), ttl); err == nil {
			t.Fatalf("租约时长 %s 应被拒绝", ttl)
		}
	}
}

func TestLeaseGuardRejectsEmptyToken(t *testing.T) {
	m := NewModel(nil)
	for _, token := range []string{"", "   "} {
		if err := m.MarkSucceeded(context.Background(), 1, token, "artifact", time.Now()); err == nil {
			t.Fatalf("空令牌 %q 应被拒绝", token)
		}
	}
}
