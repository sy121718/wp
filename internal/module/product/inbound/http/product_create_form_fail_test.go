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
	"reflect"
	"regexp"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// TestProductCreateFailSplitsByRequestKind 分档判据。
func TestProductCreateFailSplitsByRequestKind(t *testing.T) {
	h := &productPageHandle{products: &productPageReaderStub{createFormView: productCreateFormViewStub(nil)}, reader: &productPageReaderStub{createFormView: productCreateFormViewStub(nil)}}
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
	h := &productPageHandle{products: reader, reader: reader}
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
	if got := view.FormEcho; got == nil {
		t.Fatal("失败重渲染必须给非 nil 的 FormEcho（模板靠 isset(.FormEcho) 区分空表单与回填表单）")
	}
	if got := view.FormEcho.Name; got != "DRAFT SKU" {
		t.Errorf("FormEcho.Name = %q，want 「DRAFT SKU」", got)
	}
	if got := view.FormEcho.Type; got != "bundle" {
		t.Errorf("FormEcho.Type = %q，want bundle（下拉回填要能还原选项）", got)
	}
	if ids := view.FormEcho.AttributeIDs; len(ids) != 2 || ids[0] != "a1" || ids[1] != "a2" {
		t.Errorf("FormEcho.AttributeIDs = %v，want [a1 a2]（多选按值命中、保提交顺序）", ids)
	}
	if !view.FormEcho.TrackQuantity {
		t.Errorf("FormEcho.TrackQuantity = false，want true（复选框按 1/on/true/yes 判真）")
	}
	if got := view.SubmitErr; got != "错误文案" {
		t.Errorf("SubmitErr = %v，want 错误文案", got)
	}
	// 三个选项键必须在场且**非 nil**：模板用 isset + len 判断整块渲染，nil 切片会走成「缺数据」。
	if view.WarehouseOptions == nil || view.AttributeOptions == nil || view.WarehouseSKUOptions == nil {
		t.Errorf("选项键必须是空切片而不是 nil：仓库=%v 属性=%v SKU=%v",
			view.WarehouseOptions, view.AttributeOptions, view.WarehouseSKUOptions)
	}

	// 没提交任何字段时：每个字段仍是**零值**（而不是「键不存在」）—— 结构体的字段永远在场，
	// 这正是换成结构体回填后消失掉的一类故障（原先是「按清单补零值键」这一步会被漏）。
	c2, _ := newHXContext(t, "true", "") // 没提交任何字段
	empty, err := h.productCreateFormView(c2, "p1", "")
	if err != nil {
		t.Fatalf("取空表单视图失败：%v", err)
	}
	if empty.FormEcho == nil {
		t.Fatal("空提交也必须给非 nil 的 FormEcho（`isset` 判的是指针，不是有没有值）")
	}
	// 逐标量比零值（结构体带 []string 字段，整体与零值比较编译不过）。
	for name, val := range map[string]string{
		"ProjectID": empty.FormEcho.ProjectID, "Name": empty.FormEcho.Name,
		"Type": empty.FormEcho.Type, "Slug": empty.FormEcho.Slug,
		"SkuSource": empty.FormEcho.SkuSource, "SKU": empty.FormEcho.SKU,
		"WarehouseSKU": empty.FormEcho.WarehouseSKU, "ExternalSKU": empty.FormEcho.ExternalSKU,
		"DefaultPrice": empty.FormEcho.DefaultPrice, "Quantity": empty.FormEcho.Quantity,
	} {
		if val != "" {
			t.Errorf("空提交的 FormEcho.%s = %q，want 零值（结构体字段永远在场，这就是换掉手写清单的意义）", name, val)
		}
	}
	if empty.FormEcho.TrackQuantity {
		t.Error("空提交的 FormEcho.TrackQuantity = true，want false")
	}
	if len(empty.FormEcho.AttributeIDs) != 0 || len(empty.FormEcho.WarehouseIDs) != 0 {
		t.Errorf("空提交的多选回填值应为空，实际 %v / %v",
			empty.FormEcho.AttributeIDs, empty.FormEcho.WarehouseIDs)
	}
}

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

// TestProductCreateFormEchoMatchesTemplate 回填结构体与片段模板的**双向**一致性。
//
// 单向断言（「结构体字段都能在模板里找到」）只能证明 handler 没偷懒，证明不了模板读了结构体
// 没有的字段 —— 而后者才是真正会炸的方向：Jet 在 struct 上找不到字段名会报错，渲染器先渲到
// buffer、失败走 500，htmx 2.0.4 又把 5xx 判成 `swap:false` —— 用户点了保存**什么都看不到**。
//
// 换成本结构体之后，原先把守「模板 ↔ 手写清单」的那两条断言改变成了「模板 ↔ 结构体」：
// 清单不再是一份可以漂移的字符串数组，漏字段在编译期就不存在 —— 但**模板读错名字**仍然可能，
// 所以两个方向还是要钉（模板里的 `.FormEcho.X` 必须是对得上的导出字段）。
//
// 第三条钉的是另一半协议：`form:"..."` 必须与该控件的 `name="..."` 逐字一致 ——
// FillForm 按标签取值、浏览器按 name 提交，两边不同就是「用户填了、失败后回填成空」，
// 页面看起来完全正常。
func TestProductCreateFormEchoMatchesTemplate(t *testing.T) {
	raw := string(productTemplateSource(t, "product_create_form.html"))
	fragment := stripJetComments(raw)

	// —— 模板 → 结构体：模板里读的每个回填字段都要是结构体的导出字段 ——
	fieldByFormName := map[string]string{} // form 标签 → 字段名
	exported := map[string]bool{}
	echoType := reflect.TypeOf(productdto.ProductCreateFormEcho{})
	for i := 0; i < echoType.NumField(); i++ {
		field := echoType.Field(i)
		exported[field.Name] = true
		fieldByFormName[field.Tag.Get("form")] = field.Name
	}
	read := regexp.MustCompile(`\.FormEcho\.([A-Za-z][A-Za-z0-9_]*)`).
		FindAllStringSubmatch(fragment, -1)
	if len(read) == 0 {
		t.Fatal("没从片段里解析出任何回填字段访问 —— 正则或模板结构已变，这条守门成了空转")
	}
	inTemplate := map[string]bool{}
	for _, m := range read {
		inTemplate[m[1]] = true
		if !exported[m[1]] {
			t.Errorf("片段读了 .FormEcho.%s，但 dto.ProductCreateFormEcho 没有这个导出字段 —— "+
				"失败重渲染会抛 Jet 运行时错误 → 500 → htmx 不 swap，用户看不到任何反应", m[1])
		}
	}
	// —— 结构体 → 模板：每个字段模板必须真的读（否则字段在漂移，下一个人照它加标签加了个寂寞）——
	for name := range exported {
		if !inTemplate[name] {
			t.Errorf("dto.ProductCreateFormEcho.%s 没有任何模板读它 —— 结构体已与模板脱节", name)
		}
	}

	// —— 标签 ↔ 控件的 name：FillForm 的取值键必须就是浏览器提交的那个键 ——
	namesInMarkup := map[string]bool{}
	for _, m := range regexp.MustCompile(`\bname="([A-Za-z][A-Za-z0-9_]*)"`).
		FindAllStringSubmatch(fragment, -1) {
		namesInMarkup[m[1]] = true
	}
	if len(namesInMarkup) == 0 {
		t.Fatal("没从片段里解析出任何 name= —— 正则或模板结构已变，这条守门成了空转")
	}
	for formName, fieldName := range fieldByFormName {
		if formName == "" || formName == "-" {
			t.Errorf("dto.ProductCreateFormEcho.%s 缺 form 标签（FillForm 会退化成首字母小写，歧义）", fieldName)
			continue
		}
		if !namesInMarkup[formName] {
			t.Errorf("dto.ProductCreateFormEcho.%s 的 form 标签是 %q，但片段里没有 name=%q 的控件 —— "+
				"回填会读到一个永远提交不上来的键（用户填的内容在失败后丢光，且不报错）",
				fieldName, formName, formName)
		}
	}
	// 反向：片段里除 csrf_token（由页面数据键给，不走回填）之外的每个 name= 都要能回填，
	// 否则「改了 markup 忘了加字段」这条会漏。
	for name := range namesInMarkup {
		if name == "csrf_token" {
			continue
		}
		if _, ok := fieldByFormName[name]; !ok {
			t.Errorf("片段里有 name=%q 的控件，但 dto.ProductCreateFormEcho 没有对应的 form 标签 —— "+
				"这个字段提交失败后不会回填", name)
		}
	}
}

// stripJetComments 去掉模板里的 {* ... *} 注释块：注释里会写 `.FormEcho.Name` 当例子，
// 而那条正则会把例子当成真实访问，得到「结构体字段没被读」的假红。
func stripJetComments(src string) string {
	re := regexp.MustCompile(`(?s)\{\*.*?\*\}`)
	return re.ReplaceAllString(src, "")
}
