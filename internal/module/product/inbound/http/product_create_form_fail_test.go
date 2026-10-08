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

	"github.com/gin-gonic/gin"
)

// TestProductCreateFailSplitsByRequestKind 分档判据。
func TestProductCreateFailSplitsByRequestKind(t *testing.T) {
	h := &productPageHandle{}
	const msg = "捆绑商品必须填写套餐价"

	// htmx 档：200 + 片段，且**不设** HX-Redirect（那是「跳走」的形态，与「原地留住输入」相反）。
	c, rec := newHXContext(t, "true", "name=DRAFT")
	h.productCreateFail(c, "", msg)
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
	h.productCreateFail(c2, "pr1", msg)
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

// TestProductCreateFormDataCarriesEchoKeys 回填 data 的形状（片段与 handler 的协议）。
func TestProductCreateFormDataCarriesEchoKeys(t *testing.T) {
	h := &productPageHandle{}
	c, _ := newHXContext(t, "true",
		"name=DRAFT+SKU&sku=ABC_B&type=bundle&attributeIds=a1&attributeIds=a2&warehouseIds=w1&trackQuantity=1")

	data, err := h.productCreateFormData(c, "", "错误文案")
	if err != nil {
		t.Fatalf("取回填 data 失败：%v", err)
	}

	// 键名必须带 FormEcho 前缀：列表页的 `Form` 是批量改价的 pricingForm **结构体**，
	// 撞名时片段里的 `{{.Form.name}}` 命中结构体、Jet 报错并让整个片段渲染失败。
	if _, exists := data["Form"]; exists {
		t.Error("回填键不能叫 Form（与列表页批量改价的结构体撞名，会让片段渲染失败）")
	}

	echo, ok := data["FormEcho"].(gin.H)
	if !ok {
		t.Fatalf("data[\"FormEcho\"] 类型 = %T，want gin.H", data["FormEcho"])
	}
	if got := echo["name"]; got != "DRAFT SKU" {
		t.Errorf("FormEcho.name = %v，want 「DRAFT SKU」", got)
	}
	if got := echo["type"]; got != "bundle" {
		t.Errorf("FormEcho.type = %v，want bundle（下拉回填要能还原选项）", got)
	}
	// 清单里列出的字段都必须存在（补零值）：模板读缺失键会让整个片段渲染失败。
	for _, field := range productCreateFormFields {
		if _, exists := echo[field]; !exists {
			t.Errorf("FormEcho 缺字段 %q —— 模板读不到会让失败片段渲染失败（500，htmx 不 swap）", field)
		}
	}

	multi, ok := data["FormEchoMulti"].(gin.H)
	if !ok {
		t.Fatalf("data[\"FormEchoMulti\"] 类型 = %T，want gin.H", data["FormEchoMulti"])
	}
	ids, _ := multi["attributeIds"].([]string)
	if len(ids) != 2 || ids[0] != "a1" || ids[1] != "a2" {
		t.Errorf("FormEchoMulti.attributeIds = %v，want [a1 a2]（多选回填按值命中、保提交顺序）", ids)
	}

	checked, ok := data["FormEchoChecked"].(gin.H)
	if !ok {
		t.Fatalf("data[\"FormEchoChecked\"] 类型 = %T，want gin.H", data["FormEchoChecked"])
	}
	if got := checked["trackQuantity"]; got != true {
		t.Errorf("FormEchoChecked.trackQuantity = %v，want true", got)
	}

	if got := data["SubmitErr"]; got != "错误文案" {
		t.Errorf("SubmitErr = %v，want 错误文案", got)
	}
	// 三个可选列表键必须在场（值为空数组也算在场）：片段用 isset + len 判断整块渲染。
	for _, key := range []string{"WarehouseOptions", "AttributeOptions", "WarehouseSKUOptions"} {
		if _, exists := data[key]; !exists {
			t.Errorf("data 缺 %q —— 片段按 isset 判断，缺键与空列表在模板里是两条路", key)
		}
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
