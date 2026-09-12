package unit

// user_model_test.go — user model 层的语义测试（issue #36）。
//
// 这里盯的是**查询语义**而不是「方法能跑通」：软删除、唯一性查重的「看全表还是只看未注销」、
// 排除自身时的条件分组 —— 这几处判断错了都不会报错，只会给出错误的业务结论。

import (
	"context"
	"testing"

	usermodel "go_wp/internal/module/user/model"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func newModel(t *testing.T) *usermodel.UserModel {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
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

// TestCountByExistenceExcludesSelf 验证「排除自身」的条件必须整体成立。
//
// 这条用例的存在理由：username 与 email 两个条件是 OR 关系，排除自身是 AND 关系，
// 三者混在一条 SQL 里时**运算符优先级**会让语义偏离 —— 写成
// `username = ? OR (email = ? AND id <> ?)` 时，自己那行会因为「用户名对上了」
// 而被算进冲突数，于是「改资料时把自己判成冲突」，用户永远改不了自己的资料。
func TestCountByExistenceExcludesSelf(t *testing.T) {
	m := newModel(t)
	if m == nil {
		return
	}
	ctx := context.Background()
	mkUser(t, m, "alice", "alice@example.com")
	b := mkUser(t, m, "bob", "bob@example.com")

	t.Run("排除自身后不该把自己算成冲突", func(t *testing.T) {
		// b 用自己当前的用户名与邮箱查重（典型场景：只改昵称，用户名邮箱原样提交）。
		// 期望 0 —— 全部命中的都是它自己，且自身被排除。
		got, err := m.CountByExistence(ctx, "bob", "bob@example.com", b.ID)
		if err != nil {
			t.Fatalf("查重失败: %v", err)
		}
		if got != 0 {
			t.Fatalf("排除自身后应无冲突，实得 %d（说明「自身被用户名条件捞了回来」，OR/AND 分组有误）", got)
		}
	})

	t.Run("该报冲突时仍然要报", func(t *testing.T) {
		// b 想改成 a 的用户名 —— 必须报冲突（哪怕它带的邮箱是自己的）。
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
		// a 用自己原有值查重（excludeID 传 0，即注册期口径）：库里 a 自己占着 → 1
		got, err := m.CountByExistence(ctx, "alice", "alice@example.com", 0)
		if err != nil {
			t.Fatalf("查重失败: %v", err)
		}
		if got != 1 {
			t.Fatalf("不排除任何行时应命中 a 自己，实得 %d", got)
		}
	})
}
