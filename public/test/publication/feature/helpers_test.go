package feature

import (
	"context"
	"testing"
	"time"

	pubservice "go_wp/internal/module/publication/service"

	"gorm.io/gorm"
)

func strPtr(s string) *string { return &s }

func timeNow() time.Time { return time.Now().UTC() }

// checkDB 统计指定表的满足条件的行数。
func checkDB(ctx context.Context, t *testing.T, svc *pubservice.Service, table, cond string, arg any, dst *int64) {
	t.Helper()
	if err := svc.Model().ReceiptDB(ctx).Session(&gorm.Session{NewDB: true}).
		Table(table).Where(cond, arg).Count(dst).Error; err != nil {
		t.Fatalf("统计 %s 失败: %v", table, err)
	}
}
