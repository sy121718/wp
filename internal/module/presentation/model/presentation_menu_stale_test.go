package presentationmodel

// presentation_menu_stale_test.go — 菜单失效标记与待重建列表的纯逻辑判据。
//
// 三件事必须钉死，且都只能在这里钉：
//  1. 菜单 kind 白名单与 pipeline.DepKindMenu / 导航 / build 三方逐字一致 —— 任何一方
//     改名，本文件先红，而不是在生产上表现为「标了但命中 0 行」；
//  2. 菜单失效**推进** input_version（菜单译文烘进 HTML，是实体的编译输入），与其它
//     标记入口共用 staleMarkUpdates —— 这里断言的是「菜单入口确实用了那个助手」；
//  3. 入参校验在任何库访问之前完成：没有 projectID / 未知 kind 时不发 SQL。

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/clause"

	"go_wp/internal/pipeline"
)

// TestIsMenuStaleKindMatchesNavigationKinds 四个位置全认，其余一律拒绝。
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

// TestDepKindMenuMatchesPipeline 依赖种类字面量与 pipeline.DepKindMenu 逐字一致。
//
// model 不 import pipeline.SQL 层，但常量必须是同一个值：依赖键由 pipeline.MenuKey 拼出，
// 标记侧用的 kind 若与之不同，UPDATE 的 IN 子查询恒为空 — 表现为「改了菜单，详情页永远不变」
// 且没有任何错误。
func TestDepKindMenuMatchesPipeline(t *testing.T) {
	if depKindMenu != pipeline.DepKindMenu {
		t.Fatalf("depKindMenu = %q, pipeline.DepKindMenu = %q：两侧必须是同一个值", depKindMenu, pipeline.DepKindMenu)
	}
	key := pipeline.MenuKey("3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", MenuStaleKindHeader)
	if key.Kind != depKindMenu {
		t.Fatalf("pipeline.MenuKey 的 kind = %q, 与 depKindMenu 不一致", key.Kind)
	}
	if key.Key != "menu:3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f:header" {
		t.Fatalf("菜单依赖键形状变了: %q", key.Key)
	}
}

// TestStaleMarkUpdatesAdvancesInputVersion 全部标记入口（含菜单）共用：stale=true 且同语句 +1。
//
// 表达式形态是判据：读改写会被并发入口覆盖（丢版本），而「只标 stale 不推进」会让
// 过期批次的激活谓词放行旧字节。
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

// TestMarkInstanceMenuStaleTxRejectsBeforeDB 入参校验在任何库访问之前完成。
//
// tx 传 nil：若实现先碰库会 panic，本用例因此能证明「拒绝发生在发 SQL 之前」。
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

// TestListStaleInstanceIDsRejectsEmptyProject 无工程作用域直接拒绝。
//
// presentation_instances 带 FORCE 策略：无作用域的读是空集，而「列表空」会被
// menu_stale 消费侧解释成「没有待重建的实例」—— 用错误的前提宣称收敛。
func TestListStaleInstanceIDsRejectsEmptyProject(t *testing.T) {
	m := &Model{}
	if ids, err := m.ListStaleInstanceIDs(context.Background(), " "); err == nil {
		t.Fatalf("空 projectID 必须报错, got ids=%v", ids)
	}
}
