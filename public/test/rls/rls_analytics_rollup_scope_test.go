package rlstest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	analyticsmodel "go_wp/internal/module/analytics/model"
	"go_wp/pkg/rls"
)

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

	ids, err := m.ListProjectsWithViews(ctx, []string{pA, pB, pC})
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

	var raw []string
	if err = db.Raw("SELECT DISTINCT project_id::text FROM page_views").Scan(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %v", raw)
	}
}

func TestRLS_AnalyticsRollupSingleProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	pA := uuid.NewString()
	seedProject(t, db, pA, "唯一工程")
	seedPageView(t, db, pA, "/only")

	ids, err := analyticsmodel.NewModel(db).ListProjectsWithViews(ctx, []string{pA})
	if err != nil {
		t.Fatalf("取清单失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != pA {
		t.Fatalf("应只含唯一工程，实际 %v", ids)
	}
}
