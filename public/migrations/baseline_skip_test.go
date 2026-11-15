package migrations

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 水位查询失败必须向上传递，不能当成「没有基线」：
// 当成没有基线就会让已有结构的库重新进入历史迁移链（终态库上必然半路 42703）。
func TestBaselineStateFailsClosedOnQueryError(t *testing.T) {
	// 指向一个必然连不上的端口：这里要的是「查询报错」，不是可用的库。
	dsn := "host=127.0.0.1 port=1 user=none dbname=none sslmode=disable connect_timeout=1"
	// DisableAutomaticPing：让 Open 惰性成功，错误留到真正查询时暴露 ——
	// 这正是要测的分支（查询失败 vs 没有基线）。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("建立惰性连接失败：%v", err)
	}
	skippedMigrations, skippedSeeds, err := baselineState(db)
	if err == nil {
		t.Fatal("无法确认水位时必须返回错误，不能回落全量")
	}
	if skippedMigrations != nil || skippedSeeds != nil {
		t.Fatal("出错时不得返回跳过集合")
	}
	if !strings.Contains(err.Error(), "sys_baseline") {
		t.Fatalf("错误应指明 sys_baseline 查询失败，实际 %v", err)
	}
}
