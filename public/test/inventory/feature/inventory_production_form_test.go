// Package feature 生产入库表单：渲染 → 解析 → 提交的闭环（本批把丢掉的页面入口补回）。
//
// 为什么必须从**渲染出的 HTML** 里解析字段再提交：原始缺陷就是模板与 handler 的字段名对不上
// （handler 读一个模板里根本不存在的字段，空值静默通过了校验），而手写字段名的测试照样能绿 ——
// 只有把页面真实渲染出来的 name / value 拿去提交，字段名错位才会当场失败。
//
// 本文件同时钉住三件事：
//
//	· 表单渲染出的控件名集合 == handler 读取的字段集合（少一个就是「表单与 handler 不一致」）；
//	· 值口径（SKU 的「变体ID|商品ID|仓库侧裸码」三段值、来源只列内部货源）；
//	· 提交后的落账口径（库存 +2、inventory_stocks.sku_code 是裸码、流水 = in / production_in /
//	  production、单据类型 = production、重复提交被一次性幂等键挡住）。
package feature

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

// productionFormAction 生产入库表单的提交地址（与页面里渲染出的 action 一致）。
const productionFormAction = "/admin/inventory/purchases/production"

// productionFormControls 生产入库表单的**期望控件名**（测试侧字面量）。
//
// 刻意不引用被测包的 purchaseLineSkuField 常量：从被测包取就等于自证 —— 改了字段名而漏改
// 模板（或漏改 handler）时测试照样绿，而那正是原始缺陷的形态（handler 读 lineSKUCode、
// 模板里根本没有这个字段）。这里的字面量必须与 handler 读的名字对得上，对不上就红。
var productionFormControls = []string{
	"csrf_token", "projectId", "requestId", "sourceId", "lineSku", "warehouseId",
	"quantity", "unitCost", "remark",
}

// renderedFormBlock 取整页 HTML 里 action=<action> 的那个原生表单的原文。
func renderedFormBlock(t *testing.T, body, action string) string {
	t.Helper()
	marker := `action="` + action + `"`
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("渲染出的页面里没有 action=%q 的表单（表单没补回页面？）", action)
	}
	start := strings.LastIndex(body[:idx], "<form")
	if start < 0 {
		t.Fatalf("action=%q 所在标签之前没有表单开标签", action)
	}
	end := strings.Index(body[start:], "</form>")
	if end < 0 {
		t.Fatalf("action=%q 的表单没有闭合", action)
	}
	return body[start : start+end]
}

// parseRenderedForm 解析表单块里的控件：返回 name → 值，以及按文档顺序的控件名。
//
// 值同样来自渲染结果：隐藏域取它渲染出的 value，下拉取它渲染出的**第一个非空 option**。
// 这样「页面给了什么」与「测试提交了什么」是同一份数据 —— 字段名或值形状两边不一致时立刻暴露。
func parseRenderedForm(t *testing.T, block string) (url.Values, []string) {
	t.Helper()
	values := url.Values{}
	var names []string
	rest := block
	for {
		lt := strings.Index(rest, "<")
		if lt < 0 {
			break
		}
		rest = rest[lt:]
		gt := strings.Index(rest, ">")
		if gt < 0 {
			break
		}
		tag := rest[:gt+1]
		rest = rest[gt+1:]
		isInput := strings.HasPrefix(tag, "<input")
		isSelect := strings.HasPrefix(tag, "<select")
		if !isInput && !isSelect {
			continue
		}
		name := tagAttr(tag, "name")
		if name == "" {
			continue
		}
		if _, seen := values[name]; !seen {
			names = append(names, name)
		}
		if isInput {
			values.Set(name, tagAttr(tag, "value"))
			continue
		}
		// 下拉：取第一个非空 option 的 value（页面按真实候选列表渲染）。
		closeIdx := strings.Index(rest, "</select>")
		if closeIdx < 0 {
			t.Fatalf("下拉 %q 没有闭合标签", name)
		}
		inner := rest[:closeIdx]
		for {
			oi := strings.Index(inner, "<option")
			if oi < 0 {
				break
			}
			inner = inner[oi:]
			oj := strings.Index(inner, ">")
			if oj < 0 {
				break
			}
			optionTag := inner[:oj+1]
			inner = inner[oj+1:]
			if v := tagAttr(optionTag, "value"); v != "" {
				values.Set(name, v)
				break
			}
		}
	}
	return values, names
}

// tagAttr 取标签里 attr="值" 的值（不存在返回空串）。
func tagAttr(tag, attr string) string {
	marker := attr + `="`
	i := strings.Index(tag, marker)
	if i < 0 {
		return ""
	}
	rest := tag[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// renderProductionForm 重新渲染采购页并解析出生产入库表单的字段（每次渲染一个新的幂等键）。
func renderProductionForm(t *testing.T, engine *gin.Engine, projectID string) (url.Values, string) {
	t.Helper()
	rec := httptestGet(engine, "/admin/inventory/purchases?project="+projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	block := renderedFormBlock(t, rec.Body.String(), productionFormAction)
	values, names := parseRenderedForm(t, block)
	got := append([]string(nil), names...)
	sort.Strings(got)
	want := append([]string(nil), productionFormControls...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("生产入库表单的控件名与 handler 读取的字段不一致：渲染 = %v / 期望 = %v"+
			"（字段名对不上正是原始缺陷的根因：handler 读 A、模板渲染 B，值静默为空）", got, want)
	}
	return values, block
}

// copyForm 克隆一份表单（重复提交与缺字段提交各用一份，互不污染）。
func copyForm(in url.Values) url.Values {
	out := url.Values{}
	for k, vs := range in {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// receiptKindIn 按入库单号直读单据类型（断言生产入库走的是 production 单）。
func receiptKindIn(t *testing.T, f *invFixture, code string) string {
	t.Helper()
	var kind string
	if err := f.db.Raw("SELECT kind FROM inventory_purchase_receipts WHERE code = ?", code).
		Scan(&kind).Error; err != nil {
		t.Fatalf("读入库单类型失败: %v", err)
	}
	return kind
}

// productionReceiptCountIn 本工程的生产入库单数量（生产入库没有采购单，按 kind 统计）。
func productionReceiptCountIn(t *testing.T, f *invFixture) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_purchase_receipts WHERE project_id = ? AND kind = ?",
		f.projectID, inventoryenums.ReceiptKindProduction).Scan(&n).Error; err != nil {
		t.Fatalf("统计生产入库单失败: %v", err)
	}
	return n
}

// TestProductionInboundFormRenderedAndSubmitted 生产入库表单：从渲染结果解析字段并提交，
// 库存行 / 流水 / 入库单三处都按「仓库侧裸码」口径落账。
func TestProductionInboundFormRenderedAndSubmitted(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, factory, p, v := seedPurchasePage(t, f)
	bare := bareSKU(v.SKUCode, wh.Code)

	fields, block := renderProductionForm(t, engine, f.projectID)
	// 来源下拉只列内部货源（自家工厂 / 集团内关联公司）：外部供应商走采购单。
	if got := fields.Get("sourceId"); got != factory.ID {
		t.Fatalf("生产来源应落到唯一的内部货源 %s，实际 %q", factory.ID, got)
	}
	if strings.Contains(block, ext.ID) {
		t.Fatalf("生产入库的来源下拉里出现了外部货源 %s（服务端会拒绝，页面不该给这个选项）", ext.ID)
	}
	// SKU 的三段值口径 = 变体ID|商品ID|仓库侧裸码（与新建采购单的采购行同源）。
	if got, want := fields.Get("lineSku"), v.ID+"|"+p.ID+"|"+bare; got != want {
		t.Fatalf("SKU 选项值应为 %q，实际 %q", want, got)
	}
	if fields.Get("requestId") == "" {
		t.Fatalf("生产入库表单必须渲染一次性幂等键（requestId 隐藏域）")
	}
	firstRequestID := fields.Get("requestId")

	// 只补用户要填的三个值，其余字段全部来自渲染结果；SKU 这一段故意送**商品侧的带前缀编码**，
	// 入库入口负责幂等剥成裸码（inventory_stock_sku.go 是唯一落点）。
	form := copyForm(fields)
	form.Set("lineSku", v.ID+"|"+p.ID+"|"+v.SKUCode)
	form.Set("quantity", "2")
	form.Set("unitCost", "3.5")
	form.Set("remark", "自家工厂试产")
	rec := postForm(engine, productionFormAction, form)
	if rec.Code != http.StatusFound {
		t.Fatalf("生产入库表单应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "ok=1") {
		t.Fatalf("成功提交应带 ok=1 回列表，实际 %q", loc)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 2 {
		t.Fatalf("生产入库 2 后真源应为 2，实际 %d", got)
	}
	var storedSKU string
	if err := f.db.Raw("SELECT sku_code FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		v.ID, wh.ID).Scan(&storedSKU).Error; err != nil {
		t.Fatalf("读库存行 sku_code 失败: %v", err)
	}
	if storedSKU != bare {
		t.Fatalf("inventory_stocks.sku_code 应为仓库侧裸码 %q，实际 %q", bare, storedSKU)
	}
	// 流水：方向 in、原因 production_in、来源引用 = 入库单号 —— 与采购收货不是同一条。
	mv := latestMovement(t, f, v.ID)
	if mv.Direction != inventoryenums.DirectionIn || mv.ReasonCode != "production_in" ||
		mv.SourceType != inventoryenums.MovementSourceProduction || mv.SourceRef == "" {
		t.Fatalf("生产入库流水不正确（方向 / 原因 / 来源引用）：%+v", mv)
	}
	if kind := receiptKindIn(t, f, mv.SourceRef); kind != inventoryenums.ReceiptKindProduction {
		t.Fatalf("入库单类型应为 production，实际 %q", kind)
	}
	if n := productionReceiptCountIn(t, f); n != 1 {
		t.Fatalf("一次生产入库只应产生一张入库单，实际 %d", n)
	}

	// ① 重复提交同一份表单（浏览器双击 / 回退重发）：幂等键挡住，库存只加一次。
	rec = postForm(engine, productionFormAction, form)
	if rec.Code != http.StatusFound {
		t.Fatalf("重复提交应安静回列表（幂等命中），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 2 {
		t.Fatalf("重复提交不应二次加库存，实际 %d", got)
	}
	if n := productionReceiptCountIn(t, f); n != 1 {
		t.Fatalf("重复提交不应产生第二张入库单，实际 %d", n)
	}

	// ② 每次渲染都必须换一个新的幂等键 —— 否则「先收采购货、再做生产入库」的第二跳
	//    会被当成同一张单的重放而静默丢弃（幂等键在入库单上全局唯一）。
	again, _ := renderProductionForm(t, engine, f.projectID)
	if again.Get("requestId") == firstRequestID {
		t.Fatalf("两次渲染的幂等键相同（%q）：第二次提交会被当成重放", firstRequestID)
	}

	// ③ 少了 SKU 字段（例如有人只改了模板）：服务端当场拒绝，库存与单据都不动，
	//    而且文案是**可行动的中文**，不是裸 key、也不是「系统内部错误」。
	noSku := copyForm(again)
	noSku.Set("quantity", "1")
	noSku.Set("unitCost", "1")
	noSku.Del("lineSku")
	rec = postForm(engine, productionFormAction, noSku)
	if rec.Code != http.StatusFound {
		t.Fatalf("缺 SKU 的生产入库应回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("缺 SKU 的生产入库应带错误提示，实际 %q", loc)
	}
	if !strings.Contains(loc, url.QueryEscape("生产入库必须给出 SKU 变体")) {
		t.Fatalf("业务错误的文案应原样可见（中文兜底），实际 %q", loc)
	}
	if strings.Contains(loc, inventoryenums.ErrProductionVariantRequired) {
		t.Fatalf("回显里不该出现裸 key %q：%q", inventoryenums.ErrProductionVariantRequired, loc)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 2 {
		t.Fatalf("被拒绝的生产入库不应动库存，实际 %d", got)
	}
	if n := productionReceiptCountIn(t, f); n != 1 {
		t.Fatalf("被拒绝的生产入库不应留下单据，实际 %d", n)
	}
}
