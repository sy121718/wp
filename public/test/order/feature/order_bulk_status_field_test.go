package feature

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	orderBulkFormTemplate = "../../../../internal/templates/admin/order/orders.html"
	orderBulkHandlerDir   = "../../../../internal/module/order/inbound/http"

	orderBulkStatusButton = `data-write-url="/api/order/status"`
)

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
		b, err := os.ReadFile(filepath.Join(orderBulkHandlerDir, name))
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

func orderBulkWriteFormSlice(t *testing.T, src, buttonMarker string) string {
	t.Helper()
	i := strings.Index(src, buttonMarker)
	if i < 0 {
		t.Fatalf("模板 %s 里找不到按钮声明 %s", orderBulkFormTemplate, buttonMarker)
	}
	open := strings.LastIndex(src[:i], "<form")
	if open < 0 {
		t.Fatalf("按钮 %s 不在任何 <form> 里", buttonMarker)
	}
	end := strings.Index(src[i:], "</form>")
	if end < 0 {
		t.Fatalf("按钮 %s 所在的 <form> 没有闭合标签", buttonMarker)
	}
	return src[open : i+end]
}

func TestOrderBulkTargetFieldMatchesTemplate(t *testing.T) {
	raw, err := os.ReadFile(orderBulkFormTemplate)
	if err != nil {
		t.Fatalf("读取订单模板失败: %v", err)
	}
	src := string(raw)
	form := orderBulkWriteFormSlice(t, src, orderBulkStatusButton)

	selectRe := regexp.MustCompile(`<select[^>]*\sname="([^"]+)"`)
	selectMatch := selectRe.FindStringSubmatch(form)
	if selectMatch == nil {
		t.Fatalf("批量流转表单里找不到目标状态下拉（<select name=…>）")
	}
	tmplField := selectMatch[1]

	extraRe := regexp.MustCompile(`data-write-extra="([^"]*)"`)
	extraMatch := extraRe.FindStringSubmatch(form)
	if extraMatch == nil {
		t.Fatalf("批量流转按钮必须声明 data-write-extra（否则目标状态进不了请求体）")
	}
	found := false
	for _, name := range strings.Split(extraMatch[1], ",") {
		if strings.TrimSpace(name) == tmplField {
			found = true
		}
	}
	if !found {
		t.Fatalf("按钮的 data-write-extra=%q 里没有下拉的字段名 %q", extraMatch[1], tmplField)
	}

	if regexp.MustCompile(`name="status"`).MatchString(form) {
		t.Errorf("批量写表单里出现了 name=\"status\"（筛选维度）；它会与目标状态 %q 混淆，"+
			"请把它留在 GET 筛选表单里", tmplField)
	}

	tagEnd := strings.Index(form, ">")
	if tagEnd < 0 {
		t.Fatalf("批量写表单的开标签没闭合")
	}
	if strings.Contains(form[:tagEnd], "action=") {
		t.Errorf("批量写表单不应有 action（写动作由前端基座发 JSON）：%s", form[:tagEnd+1])
	}

	handlerSrc := orderBulkHandlerSource(t)
	keysRe := regexp.MustCompile(`orderStatusBulkKeys = map\[string\]bool\{([^}]*)\}`)
	keysMatch := keysRe.FindStringSubmatch(handlerSrc)
	if keysMatch == nil {
		t.Fatalf("在控制器目录 %s 里找不到 orderStatusBulkKeys 的声明", orderBulkHandlerDir)
	}
	if !strings.Contains(keysMatch[1], `"`+tmplField+`": true`) {
		t.Fatalf("批档白名单 %s 里没有模板下拉的字段名 %q —— 该字段会被当成未知字段整批 400",
			strings.TrimSpace(keysMatch[1]), tmplField)
	}
	if !strings.Contains(handlerSrc, `orderWriteStringOf(raw, "`+tmplField+`")`) {
		t.Fatalf("批档 handler 里找不到 orderWriteStringOf(raw, %q) —— 字段名两侧不一致，"+
			"目标状态会恒为空串、每条订单都被判非法", tmplField)
	}
}
