package rlstest

// rls_i18n_revision_scope_test.go — 内容译文 revision 的工程作用域（DB-009 第五批）。
//
// 缺口形状：pkg/i18n/revision.go 的 ContentRevision() 查 sys_translation 的
// max(update_time)，**不带任何工程作用域**。sys_translation 在迁移 215 的策略里放行
// 「本工程行 + project_id IS NULL 的全局行」，所以换非超级角色之后：
//   · 未设 app.project_id 时只看得见全局行 ⇒ **某个工程自己的译文写入不会推进 revision**；
//   · 而 page 侧的构建依赖（i18n:content）就靠这个值比对：revision 不变 → 依赖相等 →
//     不触发重建 → 站点长期停留在「缺译文时的回退原文」，且全程没有任何错误。
// 这正是 internal/module/page/service/page_lang.go 的 buildDependencies 注释里点名要防的
// 失效形状，只不过触发者是 RLS 而不是代码笔误。
//
// 为什么这里做「等价机制」断言而不是直接调 i18n.ContentRevisionForProject：
// 那个函数走 pkg/database 的全局单例（database.GetDB()），而 database.Init 是**一次性**的
// （`if inited { return nil }`），rls 测试用的是自己建的隔离库 —— 无法把那个单例指过来。
// 所以本用例复现它的实现（同一条 max(update_time) 查询 + 同一层 rls.InProjectScope 包装），
// 把「作用域决定可见行、进而决定 revision 变化」这件事钉住；函数本身另有语义护栏：
// 工程 id 为空时退回 ContentRevision()。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"
)

// TestRLS_ContentRevisionScopedByProject 内容译文 revision 必须按工程取。
func TestRLS_ContentRevisionScopedByProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()
	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")

	// 不带作用域：与 i18n.ContentRevision() 同一条查询（非超级角色连接）。
	unscoped := func() string {
		var latest *time.Time
		err := db.Table("sys_translation").Select("max(update_time) AS latest").Scan(&latest).Error
		if err != nil || latest == nil {
			return ""
		}
		return "trans-max-" + latest.UTC().Format(time.RFC3339Nano)
	}
	// 带作用域：与 i18n.ContentRevisionForProject 的实现逐字同形。
	scoped := func(projectID string) string {
		var latest *time.Time
		err := rls.InProjectScope(ctx, db, projectID, func(tx *gorm.DB) error {
			return tx.Table("sys_translation").Select("max(update_time) AS latest").Scan(&latest).Error
		})
		if err != nil || latest == nil {
			return ""
		}
		return "trans-max-" + latest.UTC().Format(time.RFC3339Nano)
	}

	before := scoped(pA)
	if before != "" {
		t.Fatalf("本工程尚未写入译文时 revision 应为空，实际 %q", before)
	}

	// 写入工程 A 自己的一条译文。
	const src = "夏季衬衫"
	if err := rls.InProjectScope(ctx, db, pA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO sys_translation (project_id, source_hash, context, lang, source_text, target_text, engine)
			VALUES (?, ?, 'product.name', 'en-US', ?, 'Summer Shirt', 'manual')`,
			pA, i18n.ContentHash(src), src).Error
	}); err != nil {
		t.Fatalf("写入本工程译文失败: %v", err)
	}

	after := scoped(pA)
	if after == "" || after == before {
		t.Fatalf("本工程译文写入后 revision 应推进，实际 before=%q after=%q", before, after)
	}

	// 缺口对照：**不带作用域**的查询看不见这行（它不是全局行）——
	// 所以改造前 page 的依赖登记会认为「译文没变」，从而不触发重建。
	if got := unscoped(); got != "" {
		t.Fatalf("不带作用域时不应看到本工程的译文行（revision 应为空），实际 %q", got)
	}

	// 语义护栏：工程 id 为空时退回既有实现（单工程部署 / 未接作用域的调用点行为不变）。
	if got := i18n.ContentRevisionForProject(ctx, ""); got != i18n.ContentRevision() {
		t.Fatalf("工程 id 为空应退回 ContentRevision()：%q != %q", got, i18n.ContentRevision())
	}
}
