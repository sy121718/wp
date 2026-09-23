// Package feature 库存 / 采购后台页「工程列表装载失败」的降级渲染（本批收口的三处失败出口之一）。
//
// 这两页原先在 `h.projects.List` 失败时 `c.String(500, shell.MsgInternalError)`：
// 浏览器里没有页面，只有一块写着 `MsgInternalError` 的纯文本（归口 key 未经翻译直出），
// 侧栏 / 页头 / 筛选栏整块消失 —— 运营既改不了也退不回去。
//
// 本批收成**降级渲染**（判据与 order 域订单页 / project 域主题页一致）：空数据 + 归口提示 +
// HTTP 200，页壳保留。三条断言必须同时成立，缺一条就会漏掉一类缺陷：
//
//	· `</html>` 在响应里 —— 错误分支缺键会让整页在模板中途中断（HTTP 仍 200 + 后面整块消失，
//	  见 internal/templates/CLAUDE.md「错误分支也必须给齐模板必需键」）；
//	· 归口文案出现、裸 key 不出现 —— 文案真的翻译过，而不是把 key 铺在页面上；
//	· 内部实现细节（SQLSTATE / 表名）不出现 —— 失败原因是基础设施故障，原文只进日志。
//
// 故障是**真实制造**的（把 projects 表改名，取数即报 relation does not exist），
// 不是手搓一个长得像 PG 原文的字符串。每个用例一份隔离 schema，跑完随库一起丢弃。

package feature

import (
	"net/http"
	"strings"
	"testing"
)

// breakProjectsTable 制造「工程列表读不出来」的真实故障。
func breakProjectsTable(t *testing.T, f *invFixture) {
	t.Helper()
	if err := f.db.Exec("ALTER TABLE projects RENAME TO projects_gone_for_load_test").Error; err != nil {
		t.Fatalf("制造装载失败（改表名）失败: %v", err)
	}
}

// assertLoadFailedPage 降级渲染的四条判据（页壳 / 归口文案 / 无裸 key / 空态不误导）。
func assertLoadFailedPage(t *testing.T, where, body string) {
	t.Helper()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("%s 的降级渲染没有渲染完整页（缺 </html>）：模板中途中断了", where)
	}
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("%s 的降级渲染缺归口文案：页面上没有任何提示", where)
	}
	if strings.Contains(body, "MsgInternalError") {
		t.Fatalf("%s 直出了裸归口 key `MsgInternalError`（应当显示译文）", where)
	}
	// 空态必须走「装载失败」那一档：这一次连工程列表都没读出来，再显示
	// 「还没有货源 —— 建一个」会把用户引向一个改不了任何东西的方向。
	if !strings.Contains(body, "这一页的数据没能读出来") {
		t.Fatalf("%s 的空态没走装载失败档（判据 LoadFailed 没传给模板）", where)
	}
	assertNoInternalTokens(t, where, body)
}

// TestInventorySourcesPageLoadFailedDegrades 货源页：装载失败 → 200 + 页壳完好 + 归口提示。
func TestInventorySourcesPageLoadFailedDegrades(t *testing.T) {
	engine, f := newSourcePageEngine(t)
	if engine == nil {
		return
	}
	breakProjectsTable(t, f)

	rec := httptestGet(engine, "/admin/inventory/sources")
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertLoadFailedPage(t, "货源管理页", rec.Body.String())
}

// TestInventoryPurchasesPageLoadFailedDegrades 采购入库页：同一条出口，同一组判据。
func TestInventoryPurchasesPageLoadFailedDegrades(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	breakProjectsTable(t, f)

	rec := httptestGet(engine, "/admin/inventory/purchases")
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertLoadFailedPage(t, "采购入库页", rec.Body.String())
}

// TestPageErrI18nSeeded 迁移 407 的 4 个文案位真的落库（中英各一行）。
//
// 为什么值得单独断言：模板里的中文只是 t() 兜底，词条命中时显示的是库里的值 ——
// 一条「只写了模板、忘了迁移」的文案位在中文环境里看起来完全正常，只有英文站点才暴露
// （回落中文）。按 key 逐个数中英行数：少一行即说明某一侧没登记。
func TestPageErrI18nSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	keys := []string{
		"admin.common.list.loadFailedTitle",
		"admin.common.list.loadFailedDesc",
		"admin.product_detail_template.depsMissing",
		"MsgInternalError",
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		var n int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE lang = ? AND item_key IN (?)",
			lang, keys).Scan(&n).Error; err != nil {
			t.Fatalf("查 %s 词条失败: %v", lang, err)
		}
		if n != int64(len(keys)) {
			t.Errorf("%s 词条 %d 条，期望 %d 条（缺的那条在页面上会显示裸 key 或回落另一种语言）",
				lang, n, len(keys))
		}
	}
}
