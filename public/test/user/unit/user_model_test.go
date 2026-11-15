package unit

import (
	"context"
	"testing"

	usermodel "go_wp/internal/module/user/model"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

func newModel(t *testing.T) *usermodel.UserModel {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}
	return usermodel.NewUserModel(db)
}

func mkUser(t *testing.T, m *usermodel.UserModel, username, email string) *usermodel.UserEntity {
	t.Helper()
	e := &usermodel.UserEntity{Username: username, Email: email, Status: usermodel.UserStatusActive}
	if err := m.Create(context.Background(), e); err != nil {
		t.Fatalf("建用户 %s 失败: %v", username, err)
	}
	return e
}

func TestCountByExistenceExcludesSelf(t *testing.T) {
	m := newModel(t)
	if m == nil {
		return
	}
	ctx := context.Background()
	mkUser(t, m, "alice", "alice@example.com")
	b := mkUser(t, m, "bob", "bob@example.com")

	t.Run("排除自身后不该把自己算成冲突", func(t *testing.T) {

		got, err := m.CountByExistence(ctx, "bob", "bob@example.com", b.ID)
		if err != nil {
			t.Fatalf("查重失败: %v", err)
		}
		if got != 0 {
			t.Fatalf("排除自身后应无冲突，实得 %d（说明「自身被用户名条件捞了回来」，OR/AND 分组有误）", got)
		}
	})

	t.Run("该报冲突时仍然要报", func(t *testing.T) {

		got, err := m.CountByExistence(ctx, "alice", "bob@example.com", b.ID)
		if err != nil {
			t.Fatalf("查重失败: %v", err)
		}
		if got != 1 {
			t.Fatalf("用户名为他人占用应计 1 条冲突，实得 %d", got)
		}
	})

	t.Run("邮箱被他人占用也要报", func(t *testing.T) {
		got, err := m.CountByExistence(ctx, "bob", "alice@example.com", b.ID)
		if err != nil {
			t.Fatalf("查重失败: %v", err)
		}
		if got != 1 {
			t.Fatalf("邮箱为他人占用应计 1 条冲突，实得 %d", got)
		}
	})

	t.Run("自己那条唯一索引仍然占着位置", func(t *testing.T) {

		got, err := m.CountByExistence(ctx, "alice", "alice@example.com", 0)
		if err != nil {
			t.Fatalf("查重失败: %v", err)
		}
		if got != 1 {
			t.Fatalf("不排除任何行时应命中 a 自己，实得 %d", got)
		}
	})
}
