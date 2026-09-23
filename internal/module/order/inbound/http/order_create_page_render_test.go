package orderhttp

// order_create_page_render_test.go — 后台代客建单页（docs/02-W）的渲染与字段清单回归。
//
// 三条判据：
//
//  1. 整页渲染到最后一字节（`</html>`）：Jet 读 map 里缺失的键会让整个响应失败并丢弃
//     已渲到 buffer 的内容（用户看到通用错误页，日志里有 scene=template 的渲染错误）。
//     所以「handler 给了哪些键」与「模板读了哪些键」必须严格对齐 —— 见第 2 条；
//  2. **正反双向字段清单断言**：模板读的每个回填键 / 标红字段都必须在 handler 的清单里
//     （漏列 = 渲染失败），清单里的每个键模板也必须真的在读（否则清单在悄悄漂移）；
//  3. 订单列表页的两个建单入口按权限渲染：无 order:create 时一个都不出现。
//
// 真实链路（真 handler + 真商品目录 + 真模板 + 落库行为）在
// public/test/order/feature/trade_empty_state_honesty_test.go 里覆盖，本文件只管模板契约。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/web/shell"
)

const orderCreateTemplatePath = "../../../../templates/admin/order/order_new.html"

// readOrderTemplate 读模板源码（本文件的判据都在模板源码上做，不渲染）。
func readOrderTemplate(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("读取模板 %s 失败: %v", path, err)
	}
	return string(src)
}

// orderCreatePageData 建单页的渲染数据（与 OrderCreatePage 注入的键同名）。
//
// FormEcho* 与 Invalid / FieldErrors 按 handler 的清单**全量**铺键 —— 这正是模板契约：
// 任何一个键缺失都会让整页渲染失败（而不是少显示一块）。
func orderCreatePageData() map[string]any {
	d := bulkPageBase("orders", "代客建单")
	d["Projects"] = []any{map[string]any{"ID": "p1", "Name": "站点"}}
	d["SelectedProject"] = "p1"
	d["LoadFailed"] = false
	d["NoProject"] = false
	d["FormReady"] = true
	d["CandidateKeyword"] = ""
	d["Candidates"] = []any{map[string]any{"ID": "v1", "Label": "商品 · SKU-1 · ￥12.00（可用 3）"}}
	d["CandidateCount"] = 1
	d["CandidateError"] = ""
	d["ItemRows"] = []any{map[string]any{"VariantID": "v1", "Quantity": "2"}}
	d["MaxRows"] = orderCreateMaxRows
	d["RowAddURL"] = "/admin/orders/new?project=p1&rows=2"
	d["Err"] = ""
	d["HasFieldError"] = false

	invalid := map[string]any{}
	fieldErrors := map[string]any{}
	for _, name := range orderCreateFieldNames {
		invalid[name] = false
		fieldErrors[name] = ""
	}
	d["Invalid"] = invalid
	d["FieldErrors"] = fieldErrors

	for _, key := range orderCreateEchoKeys {
		d[key] = ""
	}
	d["FormEchoRequestID"] = "req-1"
	return d
}

// TestOrderCreatePageRendersWithFullEchoKeys 建单页渲染完整 + 关键结构在。
func TestOrderCreatePageRendersWithFullEchoKeys(t *testing.T) {
	out := renderAdminTemplate(t, "admin/order/order_new.html", orderCreatePageData())
	assertContains(t, out, "</html>",
		`action="/admin/orders/create"`,
		`name="variantId"`, `name="quantity"`,
		`name="provisionGuestAccount"`, `name="sameBilling"`,
		`name="requestId"`,
		"候选 SKU 检索",
	)
	// 默认档必须**不勾选**开号（模板里的初始态就是「不开号」）。
	if strings.Contains(out, `name="provisionGuestAccount" value="1" checked`) {
		t.Error("开号复选框默认就被勾上了 —— 后台建单必须默认不开号")
	}
}

// TestOrderCreateEchoKeysMatchTemplate 回填字段清单与模板**双向**对齐。
func TestOrderCreateEchoKeysMatchTemplate(t *testing.T) {
	src := readOrderTemplate(t, orderCreateTemplatePath)

	inTemplate := map[string]bool{}
	for _, m := range regexp.MustCompile(`FormEcho[A-Za-z]+`).FindAllString(src, -1) {
		inTemplate[m] = true
	}
	declared := map[string]bool{}
	for _, k := range orderCreateEchoKeys {
		declared[k] = true
	}

	var missingDecl []string
	for k := range inTemplate {
		if !declared[k] {
			missingDecl = append(missingDecl, k)
		}
	}
	var unused []string
	for k := range declared {
		if !inTemplate[k] {
			unused = append(unused, k)
		}
	}
	sort.Strings(missingDecl)
	sort.Strings(unused)
	if len(missingDecl) > 0 {
		t.Errorf("模板读了但 orderCreateEchoKeys 没列的键（渲染时缺键 → 整页 500）：%v", missingDecl)
	}
	if len(unused) > 0 {
		t.Errorf("orderCreateEchoKeys 里模板不再读的键（清单已漂移）：%v", unused)
	}
}

// TestOrderCreateInvalidFieldsMatchTemplate 标红字段清单与模板**双向**对齐。
//
// 同一个字段既要判红（.Invalid.<name>）也要有文案位（.FieldErrors.<name>），
// 两边任一漏掉都是「页面渲染失败」而不是「样式不对」—— 两个清单必须同源。
func TestOrderCreateInvalidFieldsMatchTemplate(t *testing.T) {
	src := readOrderTemplate(t, orderCreateTemplatePath)

	invalid := map[string]bool{}
	for _, m := range regexp.MustCompile(`\.Invalid\.([A-Za-z]+)`).FindAllStringSubmatch(src, -1) {
		invalid[m[1]] = true
	}
	declared := map[string]bool{}
	for _, n := range orderCreateFieldNames {
		declared[n] = true
	}

	var missingDecl, unused []string
	for n := range invalid {
		if !declared[n] {
			missingDecl = append(missingDecl, n)
		}
	}
	for n := range declared {
		if !invalid[n] {
			unused = append(unused, n)
		}
	}
	sort.Strings(missingDecl)
	sort.Strings(unused)
	if len(missingDecl) > 0 {
		t.Errorf("模板标红了但 orderCreateFieldNames 没列的字段：%v", missingDecl)
	}
	if len(unused) > 0 {
		t.Errorf("orderCreateFieldNames 里模板不再标红的字段：%v", unused)
	}

	// 每个标红字段都要有行内文案位（只标红不说原因等于让用户猜）。
	for n := range invalid {
		if !strings.Contains(src, ".FieldErrors."+n) {
			t.Errorf("字段 %s 标红了但模板没有取 .FieldErrors.%s（没有错误文案）", n, n)
		}
	}
}

// TestAdminOrderCreatePathKeepsSidebarHighlighted 建单页归属「订单管理」菜单组。
//
// 侧栏高亮是「菜单路径 == 当前路径」**精确匹配**：子页面若不在 navPathAlias 里，
// 打开它时侧栏整组失高亮（看起来像「当前不在任何模块里」）。
func TestAdminOrderCreatePathKeepsSidebarHighlighted(t *testing.T) {
	if got := shell.NavPathFor("/admin/orders/new"); got != "/admin/orders" {
		t.Errorf("navPathAlias 缺少 /admin/orders/new → /admin/orders 的归属，侧栏会整组失高亮，实际 %q", got)
	}
}

// TestOrdersPageCreateEntryFollowsPermission 订单列表页的双入口按 order:create 渲染。
//
// 无权限时一个都不渲染（手敲 URL 提交仍会被 Casbin 拒；页面 GET 不挂 Casbin，
// 与 coupons 等页面同一口径）。
func TestOrdersPageCreateEntryFollowsPermission(t *testing.T) {
	without := renderAdminTemplate(t, "admin/order/orders.html", orderBulkPageData())
	assertAbsent(t, without, "/admin/orders/new")

	// 有权限 + 有数据：页头主行动一处。
	withRows := orderBulkPageData()
	withRows["PermSet"] = map[string]any{"order:create": true}
	head := renderAdminTemplate(t, "admin/order/orders.html", withRows)
	assertContains(t, head, "/admin/orders/new")

	// 有权限 + 空列表：页头与空态两处（双入口同 URL）。
	emptyList := orderBulkPageData()
	emptyList["PermSet"] = map[string]any{"order:create": true}
	emptyList["Rows"] = []any{}
	emptyList["Total"] = int64(0)
	emptyList["FilterActive"] = false
	emptyList["FilterLabel"] = ""
	emptyOut := renderAdminTemplate(t, "admin/order/orders.html", emptyList)
	if got := strings.Count(emptyOut, `href="/admin/orders/new`); got < 2 {
		t.Errorf("空态下建单入口应有两处（页头 + 空态），实际 %d", got)
	}
}
