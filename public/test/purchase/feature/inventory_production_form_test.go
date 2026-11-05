package feature

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	purchaseenums "go_wp/internal/module/purchase/enums"
)

const productionFormAction = "/admin/inventory/purchases/production"

var productionFormControls = []string{
	"csrf_token", "projectId", "requestId", "sourceId", "lineSku", "warehouseId",
	"quantity", "unitCost", "remark",
}

func renderedFormBlock(t *testing.T, body, action string) string {
	t.Helper()
	marker := `hx-post="` + action
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("渲染出的页面里没有 hx-post=%q 的表单（表单没补回页面？）", action)
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

func copyForm(in url.Values) url.Values {
	out := url.Values{}
	for k, vs := range in {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func receiptKindIn(t *testing.T, f *purchaseFixture, code string) string {
	t.Helper()
	var kind string
	if err := f.db.Raw("SELECT kind FROM inventory_purchase_receipts WHERE code = ?", code).
		Scan(&kind).Error; err != nil {
		t.Fatalf("读入库单类型失败: %v", err)
	}
	return kind
}

func productionReceiptCountIn(t *testing.T, f *purchaseFixture) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_purchase_receipts WHERE project_id = ? AND kind = ?",
		f.projectID, purchaseenums.ReceiptKindProduction).Scan(&n).Error; err != nil {
		t.Fatalf("统计生产入库单失败: %v", err)
	}
	return n
}

func TestProductionInboundFormRenderedAndSubmitted(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, factory, p, v := seedPurchasePage(t, f)
	bare := bareSKU(v.SKUCode, wh.Code)

	fields, block := renderProductionForm(t, engine, f.projectID)

	if got := fields.Get("sourceId"); got != factory.ID {
		t.Fatalf("生产来源应落到唯一的内部货源 %s，实际 %q", factory.ID, got)
	}
	if strings.Contains(block, ext.ID) {
		t.Fatalf("生产入库的来源下拉里出现了外部货源 %s（服务端会拒绝，页面不该给这个选项）", ext.ID)
	}

	if got, want := fields.Get("lineSku"), v.ID+"|"+p.ID+"|"+bare; got != want {
		t.Fatalf("SKU 选项值应为 %q，实际 %q", want, got)
	}
	if fields.Get("requestId") == "" {
		t.Fatalf("生产入库表单必须渲染一次性幂等键（requestId 隐藏域）")
	}
	firstRequestID := fields.Get("requestId")

	form := copyForm(fields)
	form.Set("lineSku", v.ID+"|"+p.ID+"|"+v.SKUCode)
	form.Set("quantity", "2")
	form.Set("unitCost", "3.5")
	form.Set("remark", "自家工厂试产")
	rec := postForm(engine, productionFormAction, form)

	assertInventoryJump(t, rec, "ok")
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

	mv := latestMovement(t, f, v.ID)
	if mv.Direction != purchaseenums.DirectionIn || mv.ReasonCode != "production_in" ||
		mv.SourceType != purchaseenums.MovementSourceProduction || mv.SourceRef == "" {
		t.Fatalf("生产入库流水不正确（方向 / 原因 / 来源引用）：%+v", mv)
	}
	if kind := receiptKindIn(t, f, mv.SourceRef); kind != purchaseenums.ReceiptKindProduction {
		t.Fatalf("入库单类型应为 production，实际 %q", kind)
	}
	if n := productionReceiptCountIn(t, f); n != 1 {
		t.Fatalf("一次生产入库只应产生一张入库单，实际 %d", n)
	}

	rec = postForm(engine, productionFormAction, form)
	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v.ID, wh.ID); got != 2 {
		t.Fatalf("重复提交不应二次加库存，实际 %d", got)
	}
	if n := productionReceiptCountIn(t, f); n != 1 {
		t.Fatalf("重复提交不应产生第二张入库单，实际 %d", n)
	}

	again, _ := renderProductionForm(t, engine, f.projectID)
	if again.Get("requestId") == firstRequestID {
		t.Fatalf("两次渲染的幂等键相同（%q）：第二次提交会被当成重放", firstRequestID)
	}

	noSku := copyForm(again)
	noSku.Set("quantity", "1")
	noSku.Set("unitCost", "1")
	noSku.Del("lineSku")
	rec = postForm(engine, productionFormAction, noSku)

	assertInventoryJump(t, rec, "err", "生产入库必须给出 SKU 变体")
	if strings.Contains(rec.Body.String(), purchaseenums.ErrProductionVariantRequired) {
		t.Fatalf("提示页不该出现裸 key %q：%s", purchaseenums.ErrProductionVariantRequired, rec.Body.String())
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 2 {
		t.Fatalf("被拒绝的生产入库不应动库存，实际 %d", got)
	}
	if n := productionReceiptCountIn(t, f); n != 1 {
		t.Fatalf("被拒绝的生产入库不应留下单据，实际 %d", n)
	}
}
