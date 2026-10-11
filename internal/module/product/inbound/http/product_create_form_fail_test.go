package producthttp

// product_create_form_fail_test.go — 商品建表单「失败不丢输入」的出口分档与字段清单守门。
//
// 钉四件错了会**静默失效**的事：
//   · htmx 失败 = 200 + 片段（**不能**是 302 —— htmx 的 XHR 会自己跟随 302，
//     最终响应里读不到 Location，整页 HTML 会被塞进表单的位置里）；
//   · 原生失败 = 302 + ?err= 回**本页**（不是列表页：表单在本页，错误要显示在表单上方；
//     回列表页会让用户以为「提交成功才跳走的」，还要重新找一遍新建入口）；
//   · 回填 data 的键名（撞上页面自己的 `Form`（批量改价结构体）→ 片段渲染直接报错）；
//   · 字段清单与模板的**双向**一致性 —— 反向那一条尤其重要：模板若读了一个没列进清单的
//     字段，渲染会抛 Jet 运行时错误，而渲染器先渲到 buffer、失败走 http.Error(500)，
//     htmx 2.0.4 默认又把 5xx 判成 `swap:false` —— 用户在浏览器里**看不到任何反应**。
//     （「缺键 → HTTP 200 + 后面整块消失」是渲染器加缓冲区之前的旧行为，本会话实测推翻。）

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestProductCreateFailSplitsByRequestKind 分档判据。
func TestProductCreateFailSplitsByRequestKind(t *testing.T) {
	h := &productPageHandle{products: &productPageReaderStub{createFormView: productCreateFormViewStub(nil)}}
	const msg = "捆绑商品必须填写套餐价"

	// htmx 档：200 + 片段，且**不设** HX-Redirect（那是「跳走」的形态，与「原地留住输入」相反）。
	c, rec := newHXContext(t, "true", "name=DRAFT")
	h.productCreateFail(c, "", productMsg{Fallback: msg})
	c.Writer.WriteHeaderNow() // gin 的状态码是延迟写的（WriteHeader 只记录），不刷出来读到的永远是 200
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx 档状态码 = %d，want 200（302 会被 htmx 跟随并吞掉 Location）", rec.Code)
	}
	if got, want := rec.Body.String(), "FRAG:admin/product/product_create_form.html"; got != want {
		t.Errorf("htmx 档渲染的片段 = %q，want %q", got, want)
	}
	if loc := rec.Header().Get("HX-Redirect"); loc != "" {
		t.Errorf("htmx 失败档不该发 HX-Redirect（要原地重渲染片段），实际 %q", loc)
	}

	// 原生档：整页提示（200 + err 态）回新建页，取代原先的 302 + ?err=。
	c2, rec2 := newProductJumpContext(t, "", "project=pr1", "name=DRAFT")
	h.productCreateFail(c2, "pr1", productMsg{Fallback: msg})
	c2.Writer.WriteHeaderNow()
	if rec2.Code != http.StatusOK {
		t.Fatalf("原生档状态码 = %d，want 200（提示页）", rec2.Code)
	}
	body := rec2.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("原生档应渲染 err 态提示页，body=%s", body)
	}
	if !strings.Contains(body, msg) {
		t.Fatalf("提示页应含受控文案 %q，body=%s", msg, body)
	}
	// 回跳目标必须回**新建页**（表单所在处），而不是列表页。
	if !strings.Contains(body, "/admin/products/new?") || !strings.Contains(body, "project=pr1") {
		t.Fatalf("提示页回跳应回新建页并带上工程，body=%s", body)
	}
}

// TestProductCreateFormViewCarriesEchoKeys 建表单视图的形状（模板与 handler 的协议）。
//
// 取数来自读模型（service.ProductCreateForm），回填值由控制器填 —— 视图的选项键与三份回填键
// 都必须在场：模板用 `cfFill := isset(.FormEcho)` 区分「空表单」与「回填表单」，
// 键缺失或键在但值为 nil 都会让门控与实际相反（静默：显示上次提交的值，或回填全丢）。
//
// 「回填键不能叫 Form」这一条现在是**编译期**保证：视图字段名就是 FormEcho（列表页的 Form
// 是批量改价的 productdto.PricingForm 结构体，撞名会让模板 {{.Form.name}} 命中结构体而渲染失败）。
func TestProductCreateFormViewCarriesEchoKeys(t *testing.T) {
	reader := &productPageReaderStub{createFormView: productCreateFormViewStub(nil)}
	h := &productPageHandle{products: reader}
	c, _ := newHXContext(t, "true",
		"name=DRAFT+SKU&sku=ABC_B&type=bundle&attributeIds=a1&attributeIds=a2&warehouseIds=w1&trackQuantity=1")

	view, err := h.productCreateFormView(c, "p1", "错误文案")
	if err != nil {
		t.Fatalf("取建表单视图失败：%v", err)
	}
	if view == nil {
		t.Fatal("视图恒非 nil")
	}
	// 入参转发：工程 id 读丢了会变成「所有候选都是空列表」，而页面 200、日志干净。
	if len(reader.createFormReqs) != 1 || reader.createFormReqs[0].ProjectID != "p1" {
		t.Fatalf("读模型收到的入参 = %+v，want ProjectID=p1", reader.createFormReqs)
	}
	if got := view.FormEcho["name"]; got != "DRAFT SKU" {
		t.Errorf("FormEcho[name] = %v，want 「DRAFT SKU」", got)
	}
	if got := view.FormEcho["type"]; got != "bundle" {
		t.Errorf("FormEcho[type] = %v，want bundle（下拉回填要能还原选项）", got)
	}
	ids, _ := view.FormEchoMulti["attributeIds"].([]string)
	if len(ids) != 2 || ids[0] != "a1" || ids[1] != "a2" {
		t.Errorf("FormEchoMulti[attributeIds] = %v，want [a1 a2]（多选按值命中、保提交顺序）", ids)
	}
	if got := view.FormEchoChecked["trackQuantity"]; got != true {
		t.Errorf("FormEchoChecked[trackQuantity] = %v，want true", got)
	}
	// 清单里列出的字段都必须存在（补零值）：模板读缺失键会让整块渲染失败。
	for _, field := range productCreateFormFields {
		if _, exists := view.FormEcho[field]; !exists {
			t.Errorf("FormEcho 缺字段 %q —— 模板读不到会让失败重渲染整块 500（htmx 不 swap）", field)
		}
	}
	if got := view.SubmitErr; got != "错误文案" {
		t.Errorf("SubmitErr = %v，want 错误文案", got)
	}
	// 三个选项键必须在场且**非 nil**：模板用 isset + len 判断整块渲染，nil 切片会走成「缺数据」。
	if view.WarehouseOptions == nil || view.AttributeOptions == nil || view.WarehouseSKUOptions == nil {
		t.Errorf("选项键必须是空切片而不是 nil：仓库=%v 属性=%v SKU=%v",
			view.WarehouseOptions, view.AttributeOptions, view.WarehouseSKUOptions)
	}
}

// TestProductCreateFormFieldsMatchTemplateFields 字段清单与片段模板的双向一致性。
//
// 单向断言（「清单里每个字段都能在 data 里找到」）只能证明 handler 没偷懒，
// 证明不了模板没读清单外的字段 —— 而后者才是真正会炸的方向：渲染 500 + htmx 不 swap
// = 用户点了保存什么都没发生，连错误提示都没有。所以两个方向都要钉。
// productTemplateSource 按 basename 在 templates/admin 下递归找模板源文件。
//
// 不硬编码路径：模板按后端模块分目录（admin/<模块>/x.html），一搬动硬编码就红，
// 而「跟着改路径」这个动作本身不产生验证价值（实测：一次目录重构漏了这里，
// 包级 `go test ./internal/templates/...` 全绿，模块包却红了 4 条）。
// 找不到仍然 Fatal —— 模板被移走 / 改名时必须失败，不能退化成静默跳过。
func productTemplateSource(t *testing.T, base string) []byte {
	t.Helper()
	const root = "../../../../templates/admin"
	var found string
	if err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found != "" {
			return nil
		}
		if filepath.Base(p) == base {
			found = p
		}
		return nil
	}); err != nil {
		t.Fatalf("遍历 %s 失败：%v", root, err)
	}
	if found == "" {
		t.Fatalf("templates/admin 下找不到模板 %q —— 它被移动或改名了？", base)
	}
	raw, err := os.ReadFile(found)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", found, err)
	}
	return raw
}

func TestProductCreateFormFieldsMatchTemplateFields(t *testing.T) {
	raw := productTemplateSource(t, "product_create_form.html")
	// 模板里对回填值的全部访问：.FormEcho.x / .FormEchoChecked.x / .FormEchoMulti.x
	re := regexp.MustCompile(`\.FormEcho(?:Checked|Multi)?\.([A-Za-z][A-Za-z0-9_]*)`)
	inTemplate := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		inTemplate[m[1]] = true
	}
	if len(inTemplate) == 0 {
		t.Fatal("没从片段里解析出任何回填字段访问 —— 正则或模板结构已变，这条守门成了空转")
	}
	inList := map[string]bool{}
	for _, f := range productCreateFormFields {
		inList[f] = true
	}

	// 方向一：模板读的字段必须在清单里。
	for field := range inTemplate {
		if !inList[field] {
			t.Errorf("片段读了 .FormEcho.%s，但 productCreateFormFields 没列它 —— "+
				"失败重渲染会抛 Jet 运行时错误 → 500 → htmx 不 swap，用户看不到任何反应", field)
		}
	}
	// 方向二：清单里的字段模板必须真的读（否则清单在漂移，下一个人照它补零值补了个寂寞）。
	for field := range inList {
		if !inTemplate[field] {
			t.Errorf("productCreateFormFields 列了 %q，但片段从不读它 —— 清单已与模板脱节", field)
		}
	}
}
