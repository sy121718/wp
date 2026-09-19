package pageservice

// page_dependency_lookup_test.go — 依赖键**只读反查**的纯逻辑就近单测（审计 CQ-020）。
//
// 为什么这三件事必须就地钉住（判断错了都不会报错，只会静默给出错的清单）：
//  1. 工程作用域缺失时**拒绝**而不是退化成「不限工程」——后者会把别的工程的页面混进
//     删除保护的影响面，表现为「拦下一个毫不相干的页面」；
//  2. 同一页面的活跃 / 暂存产物各声明一行时的**去重**——不去重时同一个页面在影响面里
//     出现两次，而清单长度正是运营判断「影响几处」的依据；
//  3. 输出**次序稳定**（按 id）——SQL 侧不排序（次序口径只留一份，见 model 的注释），
//     不排的话同一份数据两次渲染的行序可能不同。
//
// 门禁绿 ≠ 问题不存在：本文件不碰数据库。SQL 侧的口径（JOIN 到 pages 的软删 / 工程谓词、
// 活跃或暂存产物指针、RLS 作用域、只读不写 stale）需要真库，属 public/test/page 的范围；
// 本文件最后一条用「model 存在但 db 为 nil」的构造钉住「空依赖键根本不触库」。

import (
	"context"
	"errors"
	"strings"
	"testing"

	pagemodel "go_wp/internal/module/page/model"
)

// TestNormalizeDependencyLookup 入参归一化：工程必填、依赖键两侧对齐写入侧的 TrimSpace。
func TestNormalizeDependencyLookup(t *testing.T) {
	for _, projectID := range []string{"", " ", "	"} {
		if _, _, _, err := normalizeDependencyLookup(projectID, "content_template", "content_template:t1"); !errors.Is(err, ErrProjectRequired) {
			t.Fatalf("工程 id=%q 应返回 ErrProjectRequired（空工程不得退化成「不限工程」），实际 %v", projectID, err)
		}
	}

	pid, kind, key, err := normalizeDependencyLookup(" p1 ", " content_template ", " content_template:t1 ")
	if err != nil {
		t.Fatalf("合法入参不应报错: %v", err)
	}
	if pid != "p1" || kind != "content_template" || key != "content_template:t1" {
		t.Fatalf("归一化结果不符：pid=%q kind=%q key=%q（依赖键不 TrimSpace 时带空白的键永远查不到页面）", pid, kind, key)
	}

	// 依赖键（几乎）为空是**合法**入参，不是错误：只要任一侧为空，service 就早退返回
	// 空集合（拿空键去查等于把「依赖键缺失」变成「全站页面都引用了它」）。
	for _, c := range []struct{ kind, key string }{{"", ""}, {"", "k"}, {"content_template", ""}, {"  ", "  "}} {
		_, k, v, err := normalizeDependencyLookup("p1", c.kind, c.key)
		if err != nil {
			t.Fatalf("空依赖键（kind=%q key=%q）不应报错，实际 %v", c.kind, c.key, err)
		}
		if k != "" && v != "" {
			t.Fatalf("kind=%q key=%q 归一化后两侧都非空（k=%q v=%q）——调用方无法据「任一为空」早退", c.kind, c.key, k, v)
		}
	}
}

// TestDependencyRefsOfDedupesSortsAndTrims 行 → 投影：按页面 id 去重、跳过空白 id、按 id 排序。
func TestDependencyRefsOfDedupesSortsAndTrims(t *testing.T) {
	if got := dependencyRefsOf(nil); got != nil {
		t.Fatalf("空行集合应返回 nil（调用方按 len 判断即可），实际 %v", got)
	}
	if got := dependencyRefsOf([]pagemodel.DependencyRefRow{{ID: "   ", Title: "幽灵"}}); got != nil {
		t.Fatalf("空白 id 不应产生引用记录（它会变成一次指向不存在页面的拦截），实际 %v", got)
	}

	got := dependencyRefsOf([]pagemodel.DependencyRefRow{
		{ID: " p2 ", Title: " 关于我们 "},
		{ID: "p1", Title: "首页"},
		// 同一页面的暂存产物那一行：活跃与暂存各一行，合并后只算一条引用。
		{ID: "p2", Title: "关于我们"},
		{ID: "", Title: "幽灵"},
	})
	if len(got) != 2 {
		t.Fatalf("去重后应剩 2 条（同一页面的两行合并为一条），实际 %d 条：%+v", len(got), got)
	}
	if got[0].ID != "p1" || got[1].ID != "p2" {
		t.Fatalf("输出应按 id 升序稳定排列，实际 %s, %s", got[0].ID, got[1].ID)
	}
	if got[1].Title != "关于我们" {
		t.Fatalf("标题应 TrimSpace（同一条记录里的 id 与标题归一化口径一致），实际 %q", got[1].Title)
	}
}

// TestFindPagesByDependencyScopeAndEmptyKey 只读入口的边界：工程必填、空依赖键**不触库**。
//
// 后半条用「model 存在（非 nil）但 db 为 nil」的构造：空键若没有在进 model 之前早退，
// 就会带着 nil 句柄往下走（rls.InProjectScope / gorm 立刻现形），本用例即失败。
func TestFindPagesByDependencyScopeAndEmptyKey(t *testing.T) {
	svc := &Service{model: pagemodel.NewPageModel(nil)}
	ctx := context.Background()

	if _, err := svc.FindPagesByDependency(ctx, "", "content_template", "content_template:t1"); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("空工程 id 应返回 ErrProjectRequired，实际 %v", err)
	}
	for _, c := range []struct{ kind, key string }{{"", ""}, {"content_template", ""}, {"", "content_template:t1"}} {
		refs, err := svc.FindPagesByDependency(ctx, "p1", c.kind, c.key)
		if err != nil || refs != nil {
			t.Fatalf("空依赖键（kind=%q key=%q）应返回空集合且不触库，实际 refs=%v err=%v", c.kind, c.key, refs, err)
		}
	}
}

// TestFindPagesByDependencyWithoutModel 未装配 model 的 Service（契约未注入 / 装配缺陷）
// 必须显式失败，**不能**悄悄返回「没有引用」——那会让删除保护一路放行。
func TestFindPagesByDependencyWithoutModel(t *testing.T) {
	if _, err := (&Service{}).FindPagesByDependency(context.Background(), "p1", "content_template", "content_template:t1"); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("model 未装配应显式失败，实际 %v", err)
	}
}

// TestDependencyRefsOfKeepsDistinctPagesWithSameTitle 不同页面即使标题相同也各算一条
// （去重键是页面 id，不是标题 —— 按标题去重会把「关于我们」这类同名页面吞掉一条引用）。
func TestDependencyRefsOfKeepsDistinctPagesWithSameTitle(t *testing.T) {
	got := dependencyRefsOf([]pagemodel.DependencyRefRow{
		{ID: "p1", Title: "关于我们"},
		{ID: "p2", Title: "关于我们"},
	})
	if len(got) != 2 {
		t.Fatalf("同名不同页应各算一条引用，实际 %d 条：%+v", len(got), got)
	}
	if strings.Join([]string{got[0].ID, got[1].ID}, ",") != "p1,p2" {
		t.Fatalf("两条引用的页面 id 不符：%+v", got)
	}
}
