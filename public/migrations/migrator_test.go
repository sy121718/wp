package migrations

import (
	"strings"
	"testing"
)

func TestMigrationRegistryHasUniqueVersions(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatal(err)
	}
}

func TestCustomMigrationChecksAcceptTableParameter(t *testing.T) {
	for _, m := range allMigrations {
		if m.CheckSQL != "" && !strings.Contains(m.CheckSQL, "?") {
			t.Errorf("迁移 %s 的 CheckSQL 未接收迁移器传入的表名参数", m.Version)
		}
	}
}

func TestSplitStatementsStillHandlesDollarQuotedBlocks(t *testing.T) {
	got := SplitStatements("CREATE TABLE x (id int); DO $$ BEGIN PERFORM 1; PERFORM 2; END $$; CREATE INDEX y ON x(id);")
	if len(got) != 3 {
		t.Fatalf("期望 3 条语句，得到 %d: %#v", len(got), got)
	}
}
