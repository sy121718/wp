package schema

import (
	"strings"
	"testing"

	"go_wp/pkg/sqlsplit"
)

func TestSplitBaseSQL(t *testing.T) {
	stmts := sqlsplit.Split(mustRead("base.sql"))
	t.Logf("statements=%d", len(stmts))
	for i, s := range stmts {
		head := s
		if len(head) > 80 {
			head = head[:80]
		}
		t.Logf("%d len=%d head=%q", i, len(s), head)
	}
	for _, s := range stmts {
		if strings.HasPrefix(s, "CREATE FUNCTION") && !strings.HasSuffix(strings.TrimSpace(s), "$$") {
			t.Fatalf("function 未完整切出: tail=%q", s[len(s)-40:])
		}
	}
}
