package rlstest

// rls_analytics_rollup_scope_test.go — 访问统计汇总任务输入的工程作用域（DB-009 第七批）。
//
// 缺口形状：ListProjectsWithViews 原来是
//     SELECT DISTINCT project_id FROM page_views
// 而 page_views 带 FORCE 策略、谓词读会话变量 app.project_id —— 没有作用域时那条查询
// 恒返回空集（fail closed 不报错）。失效形态是最难发现的一种：汇总任务照常每小时跑，
// RollupRecent 返回 projects=0，日志里一行异常都没有，站点只是「历史窗口的数字一直比明细少」。
//
// 现在的实现：工程清单取自 projects 表（隔离的**主体**：没有 project_id 列、不在迁移 215
// 的清单里），再逐工程在作用域内探测明细是否存在。

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	analyticsmodel "go_wp/internal/module/analytics/model"
	"go_wp/pkg/rls"
)

// seedPageView 经显式作用域写一行访问明细（page_views 带 FORCE 策略）。
func seedPageView(t *testing.T, db *gorm.DB, projectID, path string) {
	t.Helper()
	err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO page_views "+
			"(project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at) "+
			"VALUES (?, ?, 'zh-CN', 'sess', 'vhash', '', 'desktop', 'iphash', now())",
			projectID, path).Error
	})
	if err != nil {
		t.Fatalf("写入访问明细失败（RLS 生效时写入必须承工程作用域）: %v", err)
	}
}

// TestRLS_AnalyticsRollupProjectsScoped 汇总任务的工程清单必须能列出来。
func TestRLS_AnalyticsRollupProjectsScoped(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	pA, pB, pC := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	seedProject(t, db, pC, "工程 C（无访问）")

	seedPageView(t, db, pA, "/a")
	seedPageView(t, db, pB, "/b")

	m := analyticsmodel.NewModel(db)
	ids, err := m.ListProjectsWithViews(ctx)
	if err != nil {
		t.Fatalf("取有访问的工程清单失败: %v", err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen[pA] || !seen[pB] {
		t.Fatalf("有访问明细的工程都应被列出（A/B），实际 %v", ids)
	}
	if seen[pC] {
		t.Fatalf("没有访问明细的工程不该进汇总清单，实际 %v", ids)
	}

	// 缺口对照：**不带作用域**的 DISTINCT 查询（改造前的实现）恒返回空集 ——
	// 这正是「汇总任务静默不跑」的成因。
	var raw []string
	if err = db.Raw("SELECT DISTINCT project_id::text FROM page_views").Scan(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %v", raw)
	}
}

// TestRLS_AnalyticsRollupSingleProject 单工程部署下清单仍然只含该工程。
func TestRLS_AnalyticsRollupSingleProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	pA := uuid.NewString()
	seedProject(t, db, pA, "唯一工程")
	seedPageView(t, db, pA, "/only")

	ids, err := analyticsmodel.NewModel(db).ListProjectsWithViews(ctx)
	if err != nil {
		t.Fatalf("取清单失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != pA {
		t.Fatalf("应只含唯一工程，实际 %v", ids)
	}
}
