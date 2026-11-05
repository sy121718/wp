package feature

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	orderBulkFormTemplate = "../../../../internal/templates/admin/order/orders.html"
	orderBulkHandlerDir   = "../../../../internal/module/order/inbound/http"
)

// TestOrderBulkTargetFieldMatchesTemplate 钉住「模板下拉的字段名」与「控制器读的字段名」一致。
//
// 批量流转是标准表单：下拉 name="toStatus" 由自己的承载表单提交，控制器用
// OrderStatusForm.ToStatus 读同一个名字。两侧不一致的后果是目标状态恒为空串、
// 每条订单都被判非法 —— 页面上看着像「状态机不允许」，实际是字段名对不上。
func TestOrderBulkTargetFieldMatchesTemplate(t *testing.T) {
	raw, err := os.ReadFile(orderBulkFormTemplate)
	if err != nil {
		t.Fatalf("读取订单模板失败: %v", err)
	}
	src := string(raw)

	selectRe := regexp.MustCompile(`<select[^>]*\sname="([^"]+)"[^>]*form="([^"]+)"`)
	selectMatch := selectRe.FindStringSubmatch(src)
	if selectMatch == nil {
		t.Fatalf("批量流转下拉必须在表单外、用 form 属性挂到承载表单上（<select name=… form=…>）")
	}
	tmplField, ownerForm := selectMatch[1], selectMatch[2]

	if tmplField != "toStatus" {
		t.Errorf("批量流转下拉的字段名应为 toStatus（与 OrderStatusForm.ToStatus 对齐），实际 %q", tmplField)
	}

	// 承载表单本身就在模板里，且没有 action：提交由 HTMX 声明。
	formRe := regexp.MustCompile(`<form[^>]*id="` + regexp.QuoteMeta(ownerForm) + `"[^>]*>`)
	formTag := formRe.FindString(src)
	if formTag == "" {
		t.Fatalf("模板里找不到下拉声明的承载表单 #%s", ownerForm)
	}
	if strings.Contains(formTag, "action=") {
		t.Errorf("批量写表单不应有 action（提交由 HTMX 声明）：%s", formTag)
	}

	// 批量流转按钮必须把勾选行所在的列表表单一起带上。
	buttonRe := regexp.MustCompile(`<button[^>]*hx-post="/api/order/status"[^>]*>`)
	buttonTag := buttonRe.FindString(src)
	if buttonTag == "" {
		t.Fatalf("模板里找不到 hx-post=\"/api/order/status\" 的批量流转按钮")
	}
	if !regexp.MustCompile(`hx-include="[^"]*#ord-list-form`).MatchString(buttonTag) {
		t.Errorf("批量按钮必须用 hx-include 带上勾选行所在的列表表单（#ord-list-form）：%s", buttonTag)
	}
	if !regexp.MustCompile(`hx-include="[^"]*#` + regexp.QuoteMeta(ownerForm)).MatchString(buttonTag) {
		t.Errorf("批量按钮必须用 hx-include 带上自己的承载表单 #%s：%s", ownerForm, buttonTag)
	}

	// 控制器读的字段名必须是同一个字面量。
	handlerSrc := orderBulkHandlerSource(t)
	if !strings.Contains(handlerSrc, "orderdto.OrderStatusForm") {
		t.Fatalf("控制器目录 %s 里找不到 OrderStatusForm 绑定", orderBulkHandlerDir)
	}
}

// orderBulkHandlerSource 读取控制器目录下所有非测试 .go 源码。
func orderBulkHandlerSource(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(orderBulkHandlerDir)
	if err != nil {
		t.Fatalf("读取控制器目录 %s 失败: %v", orderBulkHandlerDir, err)
	}
	var sb strings.Builder
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(orderBulkHandlerDir + "/" + name)
		if err != nil {
			t.Fatalf("读取控制器文件 %s 失败: %v", name, err)
		}
		sb.Write(b)
		sb.WriteString("\n")
		files++
	}
	if files == 0 {
		t.Fatalf("控制器目录 %s 下没有任何非测试 .go 文件", orderBulkHandlerDir)
	}
	return sb.String()
}
