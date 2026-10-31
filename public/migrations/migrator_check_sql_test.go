package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestEveryCustomCheckSQLRunsLikeMigrator(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	checked := 0
	for _, m := range migrations.All() {
		checkSQL := strings.TrimSpace(m.CheckSQL)
		if checkSQL == "" {
			continue
		}
		checked++

		check := db.Raw(checkSQL)
		if strings.Contains(checkSQL, "?") {
			check = db.Raw(checkSQL, m.TableName)
		}
		var count int64
		if err := check.Scan(&count).Error; err != nil {
			t.Errorf("迁移 %s（表 %s）的自定义 CheckSQL 执行失败：%v\nSQL: %s",
				m.Version, m.TableName, err, checkSQL)
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到任何自定义 CheckSQL：判据失效（All() 返空或字段改名）")
	}
	t.Logf("已按迁移器的调用约定执行 %d 条自定义 CheckSQL", checked)
}
