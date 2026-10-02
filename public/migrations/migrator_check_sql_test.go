package migrations_test

// migrator_check_sql_test.go — 每条自定义 CheckSQL 都能被**迁移器那样**执行。
//
// 为什么必须真库：CheckSQL 的错误不是语法错，而是**参数个数不符**
// （`expected 0 arguments, got 1`）—— 纯字符串断言看不见它，而它在启动期会让整条迁移
// 直接失败（`apply` 的 Scan 返回错误 → runAll 中断 → 其后全部注册项都跑不到）。
//
// 判据逐字复刻 migrator.apply：**只在 SQL 里出现 `?` 时才传表名**。
// 这正是「CheckSQL 必须含 `?`」那条旧断言的反面 —— 无条件传参会让按 pg_constraint /
// pg_indexes / information_schema.columns 判定的语句全部报错（474 就是这种合法写法）。

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestEveryCustomCheckSQLRunsLikeMigrator(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return // PG 不可用：库内测试已 t.Skip
	}
	checked := 0
	for _, m := range migrations.All() {
		checkSQL := strings.TrimSpace(m.CheckSQL)
		if checkSQL == "" {
			continue // 用默认检查（表存在性），迁移器行为固定
		}
		checked++
		// 与 migrator.apply 逐字一致：有 `?` 才传表名。
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
