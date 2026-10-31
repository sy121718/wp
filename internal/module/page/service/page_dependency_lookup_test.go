package pageservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	pagemodel "go_wp/internal/module/page/model"
)

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

func TestFindPagesByDependencyWithoutModel(t *testing.T) {
	if _, err := (&Service{}).FindPagesByDependency(context.Background(), "p1", "content_template", "content_template:t1"); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("model 未装配应显式失败，实际 %v", err)
	}
}

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
