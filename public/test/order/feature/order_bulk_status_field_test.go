package feature

// order_bulk_status_field_test.go — 批量状态流转的「目标状态」字段名**三处对齐**（FIX-01）。
//
// 为什么单独立一条：订单页的批量流转里有**两个名字相近的东西** ——
//   · toStatus：目标状态（下拉），批档 handler 要读的就是它；
//   · status：列表的筛选维度（GET 筛选表单里的那个）。
// 读错其中一个**不会编译失败、也不会让任何既有断言变红**：JSON 体里没有该键时读到空串，
// 于是每条订单都被判「非法目标状态」，回执「0 个已流转，N 个被跳过」。管理员看到的是
// 「一条都没做」然后反复重试 —— 这就是旧实现（读 status）的真实表现，而当时的测试恰好
// 也按 handler 的口径构造请求，两边一起错。
//
// 通道换过两轮，本用例守的**边界没变**，但载体变了：
//   · 旧：页面 POST + form action 的 query 带回跳上下文，字段名在模板下拉与 c.PostForm 之间对齐；
//   · 现在：前端基座 fetch（static/js/ui/write-base.js）把按钮声明的额外字段拼进 JSON 体，
//     所以「模板下拉的 name」要同时对两处负责 —— **按钮的 data-write-extra**（唯一的发送通道）
//     与 **Go 侧批档白名单 + 读取字面量**。三处任何一处改名，另一处不同改就静默失效。
//
// 与旧用例的另一处差异：批量表单现在**没有 action**（写动作不走原生提交；禁用 JS 时按钮
// 是 hidden 的，不会退化成「GET 当前页」这种看起来像成功的空动作），所以不再断言 action query。

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
	// orderBulkStatusButton 批量状态流转按钮上的端点声明：用它定位「哪张表单是批量写表单」。
	orderBulkStatusButton = `data-write-url="/api/order/status"`
)

// orderBulkHandlerSource 把订单控制器目录下所有非测试 Go 文件拼成一份源码。
//
// 不硬编码文件名：OrderBulkStatus 从 order_page.go 拆到 order_list_page.go、再拆到
// order_bulk_write.go 后，旧用例读的是已经不存在的文件，直接 Fatal —— 而它想守的判据
// （字段名对齐）根本没被执行到。目录扫描对「下一次搬家」免疫：只要这个函数还在目录里，
// 判据就照常生效。
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

// orderBulkWriteFormSlice 取「包含某条按钮声明的那张表单」的片段（含开闭标签）。
//
// 不硬编码行号或表单序号：批量条是 `{{if len(orders) > 0}}` 里的，且文件里还有筛选表单，
// 按「按钮声明在谁里面」定位是唯一稳的找法 —— 找不到就直说缺失，不静默跳过。
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

// TestOrderBulkTargetFieldMatchesTemplate 批量目标状态字段名在「模板 → 前端基座 → 批档 handler」
// 三处一致，且批量写表单里不得混进同名的筛选维度 status。
func TestOrderBulkTargetFieldMatchesTemplate(t *testing.T) {
	raw, err := os.ReadFile(orderBulkFormTemplate)
	if err != nil {
		t.Fatalf("读取订单模板失败: %v", err)
	}
	src := string(raw)
	form := orderBulkWriteFormSlice(t, src, orderBulkStatusButton)

	// ① 模板：目标状态下拉的 name。
	selectRe := regexp.MustCompile(`<select[^>]*\sname="([^"]+)"`)
	selectMatch := selectRe.FindStringSubmatch(form)
	if selectMatch == nil {
		t.Fatalf("批量流转表单里找不到目标状态下拉（<select name=…>）")
	}
	tmplField := selectMatch[1]

	// ② 前端基座：按钮的 data-write-extra 是把这个字段送进 JSON 体的**唯一**通道
	//（attrFrom 只收按钮/表单上的具名声明，没声明就不会出现在 payload 里）。
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

	// ③ 反向：批量写表单里不得出现筛选维度 status —— 它一旦混进来，「读错字段」的边界就回来了。
	if regexp.MustCompile(`name="status"`).MatchString(form) {
		t.Errorf("批量写表单里出现了 name=\"status\"（筛选维度）；它会与目标状态 %q 混淆，"+
			"请把它留在 GET 筛选表单里", tmplField)
	}

	// ④ 反向：批量表单不得有 action —— 写动作不走原生提交，有 action 会让禁用 JS 时
	// 退化成「GET 当前页」这种看起来像成功的空动作。
	tagEnd := strings.Index(form, ">")
	if tagEnd < 0 {
		t.Fatalf("批量写表单的开标签没闭合")
	}
	if strings.Contains(form[:tagEnd], "action=") {
		t.Errorf("批量写表单不应有 action（写动作由前端基座发 JSON）：%s", form[:tagEnd+1])
	}

	// ⑤ Go 侧两处字面量：批档白名单集合 + 读取该字段的调用。
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
