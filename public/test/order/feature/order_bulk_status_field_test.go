package feature

// order_bulk_status_field_test.go — 批量状态流转的「目标状态」字段名两侧对齐（FIX-01）。
//
// 为什么单独立一条：订单页的批量流转表单里有**两个名字相近的字段** ——
//   · name="toStatus"：目标状态（下拉），OrderBulkStatus 要读的就是它；
//   · name="status"：回跳时保留的筛选维度（hidden），与流转无关。
//
// 读错其中一个**不会编译失败、也不会让任何既有断言变红**：PostForm 取不到就得到空串，
// 于是每条订单都被判「非法目标状态」，回执「0 个已流转，N 个被跳过」。管理员看到的是
// 「一条都没做」然后反复重试 —— 这就是旧实现（读 status）的真实表现，而当时的测试
// 恰好也按 handler 的口径构造请求，两边一起错。
//
// 所以这里把**模板里那个下拉的字段名**与**handler 实际读取的字段名**直接钉在一起：
// 任意一侧改名，另一侧必须同改，否则本用例红。

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	orderBulkFormTemplate = "../../../../internal/templates/admin/order/orders.html"
	orderBulkFormHandler  = "../../../../internal/module/order/inbound/http/order_page_handle.go"
)

// TestOrderBulkTargetFieldMatchesTemplate 批量目标状态下拉的 name == OrderBulkStatus 读取的字段名。
func TestOrderBulkTargetFieldMatchesTemplate(t *testing.T) {
	tmplRaw, err := os.ReadFile(orderBulkFormTemplate)
	if err != nil {
		t.Fatalf("读取订单模板失败: %v", err)
	}
	formRe := regexp.MustCompile(`(?s)<form[^>]*action="/admin/orders/bulk-status"[^>]*>(.*?)</form>`)
	form := formRe.FindStringSubmatch(string(tmplRaw))
	if form == nil {
		t.Fatalf("模板 %s 里找不到 action=\"/admin/orders/bulk-status\" 的表单", orderBulkFormTemplate)
	}
	selectRe := regexp.MustCompile(`<select[^>]*\sname="([^"]+)"`)
	selectMatch := selectRe.FindStringSubmatch(form[1])
	if selectMatch == nil {
		t.Fatalf("批量流转表单里找不到目标状态下拉（<select name=…>）")
	}
	tmplField := selectMatch[1]

	srcRaw, err := os.ReadFile(orderBulkFormHandler)
	if err != nil {
		t.Fatalf("读取订单 handler 失败: %v", err)
	}
	bodyRe := regexp.MustCompile(`(?s)func \(h \*orderPageHandle\) OrderBulkStatus\(c \*gin\.Context\) \{(.*?)\n\}`)
	body := bodyRe.FindStringSubmatch(string(srcRaw))
	if body == nil {
		t.Fatalf("在 %s 里找不到 OrderBulkStatus 的函数体", orderBulkFormHandler)
	}
	// 只在「目标状态」这一行上比对：PostForm 的读取方式变了就更新本用例（宁可红，不要静默空跑）。
	fieldRe := regexp.MustCompile(`toStatus\s*:=\s*strings\.TrimSpace\(c\.PostForm\("([^"]+)"\)\)`)
	fieldMatch := fieldRe.FindStringSubmatch(body[1])
	if fieldMatch == nil {
		t.Fatalf("OrderBulkStatus 里找不到 toStatus := strings.TrimSpace(c.PostForm(...))")
	}
	handlerField := fieldMatch[1]

	if tmplField != handlerField {
		t.Fatalf("批量目标状态字段名不一致：模板下拉 name=%q，OrderBulkStatus 读的是 %q —— "+
			"两侧必须同名，否则目标状态恒为空串、整批被判非法状态", tmplField, handlerField)
	}
	// 反向钉住前提：批量表单里必须另有 status（回跳筛选值）。它一旦消失，
	// 本用例守的「两个相近字段别混」这条边界就不存在了，断言应连同删除。
	if !strings.Contains(form[1], `name="status"`) {
		t.Errorf(`批量流转表单里不再有回跳筛选字段 name="status"；若它已被移除，请一并复核本用例是否还需要`)
	}
}
