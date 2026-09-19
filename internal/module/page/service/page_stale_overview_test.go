package pageservice

// page_stale_overview_test.go — 待重建反查面的纯逻辑就近单测（审计 CQ-020）。
//
// 这些函数都不碰数据库，但都在回答运营**会照着做决定**的问题：
// 「这次改动影响了哪几页」「清单是不是被截断了」「同一页在三处显示的名字是否一致」。
// 判断错了不会崩、不会报错，只会让影响面清单少几行或多几行 —— 这类偏差在
// feature 测试里最难被发现（断言通常只看「有没有出现这一行」），所以就近钉住。
//
// 门禁绿 ≠ 问题不存在：本文件只覆盖纯函数（合并 / 排序 / 截断 / 可读标识 / 日志样本），
// 真正的 SQL 口径（staleTitleExpr 的 JSONB 取值、RLS 作用域）由
// public/test/page 的 feature 用例与 RLS 用例覆盖 —— 那些需要真库，不在本包。
//
// feature 测试命令（不在本批执行，供主代理统一跑）：
//   go test ./public/test/page/... -count=1
//   go test ./public/test/rls/... -count=1 -run TestRLS_PageFanout

import (
	"strings"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
)

func staleTime(min int) time.Time {
	return time.Date(2026, 1, 1, 12, min, 0, 0, time.UTC)
}

func strPtr(s string) *string { return &s }

func staleRow(id string, updatedMin int, title, draft string, active *string) pagemodel.StalePageRow {
	return pagemodel.StalePageRow{
		ID: id, ProjectID: "p1", Title: title, DraftPath: draft,
		ActivePath: active, Stale: true, UpdatedAt: staleTime(updatedMin),
	}
}

// TestMergeStaleRowsEmptyAndLimit 空集合与 limit 生效：截断只截尾部，不改变次序。
func TestMergeStaleRowsEmptyAndLimit(t *testing.T) {
	if got := mergeStaleRows(nil, 5, pagemodel.StaleOrderUpdateTime, true); len(got) != 0 {
		t.Fatalf("空集合应返回空切片，实际 %d 条", len(got))
	}
	parts := [][]pagemodel.StalePageRow{{
		staleRow("a", 1, "", "/a", nil),
		staleRow("b", 2, "", "/b", nil),
		staleRow("c", 3, "", "/c", nil),
	}}
	got := mergeStaleRows(parts, 2, pagemodel.StaleOrderUpdateTime, true)
	if len(got) != 2 {
		t.Fatalf("limit=2 应截断到 2 条，实际 %d 条", len(got))
	}
	// 降序：最近的（c, 3 分）在前。
	if got[0].ID != "c" || got[1].ID != "b" {
		t.Fatalf("降序截断应保留最近的两条，实际 %s, %s", got[0].ID, got[1].ID)
	}
}

// TestMergeStaleRowsAcrossProjects 跨工程合并：全局序必须重排，而不是各工程片段首尾相接。
func TestMergeStaleRowsAcrossProjects(t *testing.T) {
	// 工程 A 的片段比工程 B 的「新」，但遍历顺序是 A 在前 —— 若直接拼接，
	// 结果恰好正确；把 A 放后面才能证明真的重排了。
	parts := [][]pagemodel.StalePageRow{
		{staleRow("p1-old", 1, "", "/old", nil)},
		{staleRow("p2-new", 9, "", "/new", nil), staleRow("p2-mid", 5, "", "/mid", nil)},
	}
	got := mergeStaleRows(parts, 10, pagemodel.StaleOrderUpdateTime, true)
	want := []string{"p2-new", "p2-mid", "p1-old"}
	if len(got) != len(want) {
		t.Fatalf("合并后应有 %d 条，实际 %d 条", len(want), len(got))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("全局降序第 %d 条应为 %s，实际 %s", i, want[i], got[i].ID)
		}
	}
}

// TestMergeStaleRowsStableTieBreakByID 同值行的次序由 id 兜底（与 SQL 的 ", id ASC" 对齐）。
func TestMergeStaleRowsStableTieBreakByID(t *testing.T) {
	parts := [][]pagemodel.StalePageRow{{
		staleRow("z-id", 7, "", "/z", nil),
		staleRow("a-id", 7, "", "/a", nil),
	}}
	got := mergeStaleRows(parts, 10, pagemodel.StaleOrderUpdateTime, true)
	if got[0].ID != "a-id" || got[1].ID != "z-id" {
		t.Fatalf("同标记时刻应按 id 升序兜底，实际 %s, %s", got[0].ID, got[1].ID)
	}
}

// TestStaleRowLessDirections 排序方向与键：升序 / 降序 / 路径键 / 标题键。
func TestStaleRowLessDirections(t *testing.T) {
	newer := staleRow("newer", 9, "B", "/b", nil)
	older := staleRow("older", 1, "A", "/a", nil)
	cases := []struct {
		name       string
		a, b       pagemodel.StalePageRow
		orderBy    string
		descending bool
		want       bool
	}{
		{"标记时间升序", older, newer, pagemodel.StaleOrderUpdateTime, false, true},
		{"标记时间降序", newer, older, pagemodel.StaleOrderUpdateTime, true, true},
		{"标记时间降序的反向不成立", older, newer, pagemodel.StaleOrderUpdateTime, true, false},
		{"草稿路径升序", older, newer, pagemodel.StaleOrderDraftPath, false, true},
		{"标题升序", older, newer, pagemodel.StaleOrderTitle, false, true},
		// id 键（uuid）只用于「需要与 SQL 完全一致次序」的场景：用两个明确的 id 值验证字节序，
		// 别拿 "older"/"newer" 这类名字当证据 —— 字母序与语义相反，测出来的结论会误导人。
		{"id 升序", staleRow("a-id", 5, "", "/a", nil), staleRow("b-id", 5, "", "/b", nil), pagemodel.StaleOrderID, false, true},
	}
	for _, c := range cases {
		if got := staleRowLess(c.a, c.b, c.orderBy, c.descending); got != c.want {
			t.Errorf("%s: staleRowLess=%v，期望 %v", c.name, got, c.want)
		}
	}
}

// TestStaleDisplayPath 页面可读标识：已上线路径优先，其次草稿路径，都没有时退回 id。
//
// 这条口径必须与 content 的 stalePagePath / block 的 blockStalePagePath 一致 ——
// 同一个页面在三处显示成三个名字，读的人会以为是三个页面。
func TestStaleDisplayPath(t *testing.T) {
	cases := []struct {
		name string
		row  pagemodel.StalePageRow
		want string
	}{
		{"已上线优先", staleRow("id-1", 1, "", "/draft", strPtr("/live")), "/live"},
		{"未发布回落草稿", staleRow("id-2", 1, "", "/draft", nil), "/draft"},
		{"上线路径为空白回落草稿", staleRow("id-3", 1, "", "/draft", strPtr("  ")), "/draft"},
		{"都没有退回 id", staleRow("id-4", 1, "", "  ", nil), "id-4"},
	}
	for _, c := range cases {
		if got := staleDisplayPath(c.row); got != c.want {
			t.Errorf("%s: staleDisplayPath=%q，期望 %q", c.name, got, c.want)
		}
	}
	// Published 与可读标识是同一条判据的两个投影，不能分叉。
	if stalePublished(staleRow("id-5", 1, "", "/draft", nil)) {
		t.Error("未发布页面 stalePublished 应为 false")
	}
	if !stalePublished(staleRow("id-6", 1, "", "/draft", strPtr("/live"))) {
		t.Error("已发布页面 stalePublished 应为 true")
	}
}

// TestFormatStaleImpactSample 日志样本：标题优先，缺失时回落路径，不编占位文案。
func TestFormatStaleImpactSample(t *testing.T) {
	pages := []pagedto.StalePageResp{
		{Title: "关于我们", Path: "/about"},
		{Title: "", Path: "/contact"},
		{Title: "/same", Path: "/same"},
	}
	got := formatStaleImpactSample(pages)
	want := "关于我们 (/about); /contact; /same"
	if got != want {
		t.Fatalf("样本格式化结果不符：%q，期望 %q", got, want)
	}
	if s := formatStaleImpactSample(nil); s != "" {
		t.Fatalf("空集合应格式化为空串，实际 %q", s)
	}
	if strings.Contains(got, "(/same)") {
		t.Fatal("标题与路径相同时不应重复输出括号里的路径")
	}
}

// TestNormalizeStaleInput 排序键与 limit 的归一化口径（与调用方共用同一份实现）。
func TestNormalizeStaleInput(t *testing.T) {
	if key, err := pagemodel.NormalizeStaleOrder(""); err != nil || key != pagemodel.StaleOrderUpdateTime {
		t.Fatalf("空排序键应取默认标记时间，实际 %q / %v", key, err)
	}
	if _, err := pagemodel.NormalizeStaleOrder("update_time; DROP TABLE pages"); err == nil {
		t.Fatal("白名单外的排序键必须报错，不能静默回落")
	}
	if got := pagemodel.NormalizeStaleListLimit(0); got != pagemodel.DefaultStaleListLimit {
		t.Fatalf("limit<=0 应取默认 %d，实际 %d", pagemodel.DefaultStaleListLimit, got)
	}
	if got := pagemodel.NormalizeStaleListLimit(pagemodel.MaxStaleListLimit + 1); got != pagemodel.MaxStaleListLimit {
		t.Fatalf("超上限应封顶到 %d，实际 %d", pagemodel.MaxStaleListLimit, got)
	}
	if got := pagemodel.NormalizeStaleListLimit(3); got != 3 {
		t.Fatalf("合法 limit 应原样返回，实际 %d", got)
	}
}
