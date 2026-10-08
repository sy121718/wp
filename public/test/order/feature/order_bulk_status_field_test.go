package feature

// order_bulk_status_field_test.go — 批量状态流转的「目标状态」字段名两侧对齐（FIX-01）。
//
// 为什么单独立一条：订单页的批量流转表单里有**两个名字相近的字段** ——
//   · name="toStatus"：目标状态（下拉），OrderBulkStatus 要读的就是它；
//   · status：回跳时保留的筛选维度。它**不再是表单里的隐藏域**（隐藏域那套随
//     「写动作的结论不经 URL」一起删除），而是拼在批量表单 action 的 query 上
//     （`action="/admin/orders/bulk-status?{{listQuery}}&status={{.FilterStatus}}"`）——
//     服务端由 shell.BackPath 按白名单从**本次请求的 query** 读回，不再从隐藏域解析整串 URL。
//
// 读错其中一个**不会编译失败、也不会让任何既有断言变红**：PostForm 取不到就得到空串，
// 于是每条订单都被判「非法目标状态」，回执「0 个已流转，N 个被跳过」。管理员看到的是
// 「一条都没做」然后反复重试 —— 这就是旧实现（读 status）的真实表现，而当时的测试
// 恰好也按 handler 的口径构造请求，两边一起错。
//
// 所以这里把**模板里那个下拉的字段名**与**handler 实际读取的字段名**直接钉在一起：
// 任意一侧改名，另一侧必须同改，否则本用例红。
//
// 本轮契约变化（架构改造后）：
//   - 控制器文件从 order_page.go 拆成 order_list_page.go + order_create_page.go + order_shared.go。
//     本用例改为**扫描整个控制器目录**，不再硬编码单个文件名 —— 否则下一次搬迁又会变成
//     「读不到文件 → Fatal」，而不是一条指向真实断言的失败。
//   - 批量表单的 action 现在带 query（回跳上下文），不再要求 action 恰好是裸路径。

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
)

// orderBulkHandlerSource 把订单控制器目录下所有非测试 Go 文件拼成一份源码。
//
// 不硬编码文件名：OrderBulkStatus 从 order_page.go 拆到 order_list_page.go 后，旧用例读的是
// 已经不存在的文件，直接 Fatal —— 而它想守的判据（字段名两侧对齐）根本没被执行到。
// 目录扫描对「下一次搬家」免疫：只要这个函数还在目录里，判据就照常生效。
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

// TestOrderBulkTargetFieldMatchesTemplate 批量目标状态下拉的 name == OrderBulkStatus 读取的字段名。
func TestOrderBulkTargetFieldMatchesTemplate(t *testing.T) {
	tmplRaw, err := os.ReadFile(orderBulkFormTemplate)
	if err != nil {
		t.Fatalf("读取订单模板失败: %v", err)
	}
	// action 现在带 query（回跳上下文），所以路径后允许任意非引号字符：
	// 匹配 `/admin/orders/bulk-status` 与 `/admin/orders/bulk-status?…` 两种形态。
	formRe := regexp.MustCompile(`(?s)<form[^>]*action="/admin/orders/bulk-status[^"]*"[^>]*>(.*?)</form>`)
	form := formRe.FindStringSubmatch(string(tmplRaw))
	if form == nil {
		t.Fatalf("模板 %s 里找不到 action=\"/admin/orders/bulk-status\" 的表单（含带 query 的形态）", orderBulkFormTemplate)
	}
	selectRe := regexp.MustCompile(`<select[^>]*\sname="([^"]+)"`)
	selectMatch := selectRe.FindStringSubmatch(form[1])
	if selectMatch == nil {
		t.Fatalf("批量流转表单里找不到目标状态下拉（<select name=…>）")
	}
	tmplField := selectMatch[1]

	bodyRe := regexp.MustCompile(`(?s)func \(h \*orderPageHandle\) OrderBulkStatus\(c \*gin\.Context\) \{(.*?)\n\}`)
	body := bodyRe.FindStringSubmatch(orderBulkHandlerSource(t))
	if body == nil {
		t.Fatalf("在控制器目录 %s 里找不到 OrderBulkStatus 的函数体", orderBulkHandlerDir)
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
	// 反向钉住前提：回跳筛选值必须仍在批量表单的 action query 上（现在它是唯一载体）。
	// 它一旦消失，本用例守的「两个相近字段别混」这条边界就不存在了，断言应连同删除。
	if !strings.Contains(form[0], "status=") {
		t.Errorf("批量流转表单的 action query 里不再带回跳筛选值 status=…；" +
			`若回跳上下文已改走别的通道，请一并复核本用例是否还需要`)
	}
}
