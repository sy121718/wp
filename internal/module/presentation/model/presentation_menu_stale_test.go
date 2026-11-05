package presentationmodel

import (
	"context"
	publish "go_wp/pkg/publish"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/clause"
)

func TestIsMenuStaleKindMatchesNavigationKinds(t *testing.T) {
	for _, kind := range []string{
		MenuStaleKindHeader, MenuStaleKindHeaderMobile, MenuStaleKindFooter, MenuStaleKindFooterMobile,
	} {
		if !IsMenuStaleKind(kind) {
			t.Fatalf("%q 应被认作合法菜单位置", kind)
		}
	}
	for _, kind := range []string{"", "Header", "sidebar", "header ", "menu"} {
		if IsMenuStaleKind(kind) {
			t.Fatalf("%q 不该被认作合法菜单位置（未知位置必须报错，不能静默全站标记）", kind)
		}
	}
}

func TestDepKindMenuMatchesPipeline(t *testing.T) {
	if depKindMenu != publish.DepKindMenu {
		t.Fatalf("depKindMenu = %q, publish.DepKindMenu = %q：两侧必须是同一个值", depKindMenu, publish.DepKindMenu)
	}
	key := publish.MenuKey("3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", MenuStaleKindHeader)
	if key.Kind != depKindMenu {
		t.Fatalf("publish.MenuKey 的 kind = %q, 与 depKindMenu 不一致", key.Kind)
	}
	if key.Key != "menu:3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f:header" {
		t.Fatalf("菜单依赖键形状变了: %q", key.Key)
	}
}

func TestStaleMarkUpdatesAdvancesInputVersion(t *testing.T) {
	at := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	updates := staleMarkUpdates(at)
	if v, ok := updates["stale"].(bool); !ok || !v {
		t.Fatalf("staleMarkUpdates 必须把 stale 置 true, got %#v", updates["stale"])
	}
	expr, ok := updates["input_version"].(clause.Expr)
	if !ok {
		t.Fatalf("input_version 必须以 gorm.Expr 写库（读改写会被并发入口覆盖）, got %T", updates["input_version"])
	}
	if !strings.Contains(strings.ToLower(expr.SQL), "input_version + 1") {
		t.Fatalf("input_version 的推进表达式必须是 input_version + 1, got %q", expr.SQL)
	}
	if !at.Equal(updates["update_time"].(time.Time)) {
		t.Fatalf("update_time 应原样落库, got %#v", updates["update_time"])
	}
}

func TestMarkInstanceMenuStaleTxRejectsBeforeDB(t *testing.T) {
	m := &Model{}
	at := time.Now().UTC()

	if _, err := m.MarkInstanceMenuStaleTx(context.Background(), nil, "  ", MenuStaleKindHeader, at); err == nil {
		t.Fatal("空 projectID 必须报错（另开事务会让外层未提交的译文不可见）")
	}
	if _, err := m.MarkInstanceMenuStaleTx(context.Background(), nil, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", "sidebar", at); err == nil {
		t.Fatal("未知菜单位置必须报错（不能静默把全站实例标脏）")
	}
}

func TestListStaleInstanceIDsRejectsEmptyProject(t *testing.T) {
	m := &Model{}
	if ids, err := m.ListStaleInstanceIDs(context.Background(), " "); err == nil {
		t.Fatalf("空 projectID 必须报错, got ids=%v", ids)
	}
}
