package producthttp

// product_attribute_form_fail_test.go — 属性页两个抽屉表单「提交失败原地留住输入」的出口分档守门。
//
// 只钉错了一件就**静默失效**的事：
//   - htmx 失败 = 200 + 片段（不能是 302 —— htmx 的 XHR 会自己跟随 302，最终响应里读不到
//     Location，整页 HTML 会被塞进抽屉里）；
//   - 原生失败 = 302 + ?err=（无 JS 环境的既有行为一字不改）；
//   - 回填 data 的键名与字段清单对齐（缺字段不再是「少渲一块」，而是整块渲染失败 → 500，
//     而 htmx 对 5xx 不 swap：用户点保存后**什么都看不到**）。
//
// 另钉两件本域特有的：
//   - 勾选态按「同名多值里存在 1」判（同名隐藏域打底让「字段是否存在」恒真）；
//   - 抽屉形态由表单自己的隐藏域申报，缺了它失败重渲染后「取消」按钮就消失了。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/shell"
)

// —— 渲染捕获（把失败出口真正交给模板的那份 data 截下来）——

// captureRender 一个只记录「渲染了哪个模板、带什么 data」的渲染器。
//
// 为什么要截 data：本批改的正是「失败出口往片段 data 里并入了什么」（FormEcho* / SubmitErr /
// 抽屉形态），而这些键只在渲染那一刻存在。断言它们最直接的方式就是在这里截住 ——
// 在测试里再抄一遍并入逻辑等于测了个复制品。
type captureRender struct {
	name string
	data any
}

func (r *captureRender) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data
	return discardRender{}
}

type discardRender struct{}

func (discardRender) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (discardRender) Render(w http.ResponseWriter) error { return nil }

// newAttrCaptureContext 造一个带 HX-Request 头的请求上下文，并把渲染 data 留下。
func newAttrCaptureContext(t *testing.T, hxHeader, body string) (*gin.Context, *httptest.ResponseRecorder, *captureRender) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	cap := &captureRender{}
	engine.HTMLRender = cap
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/product-attributes/create", strings.NewReader(body))
	if hxHeader != "" {
		c.Request.Header.Set("HX-Request", hxHeader)
	}
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c, rec, cap
}

// ——— 提交样本 ———

// attrGroupSubmitBody 一份「新建属性组」的提交（勾了参与变体、填了一行属性值）。
func attrGroupSubmitBody() string {
	form := url.Values{}
	form.Set("csrf_token", "tok-1")
	form.Set("projectId", "pr1")
	form.Set("name", "颜色")
	form.Set("key", "color")
	form.Set("sort", "3")
	// 勾选：同名隐藏域打底（0）在前、复选框（1）在后 —— 浏览器提交的真实形态。
	form.Add("isVariation", "0")
	form.Add("isVariation", "1")
	form.Set("inDrawer", "1")
	form.Set("values[0].label", "红")
	form.Set("values[0].key", "red")
	form.Add("values[0].enabled", "0")
	form.Add("values[0].enabled", "1")
	return form.Encode()
}

// attrValuesSubmitBody 一份「保存属性值」的提交：两行，一行启用、一行被用户明确取消勾选。
func attrValuesSubmitBody() string {
	form := url.Values{}
	form.Set("csrf_token", "tok-1")
	form.Set("projectId", "pr1")
	form.Set("id", "a1")
	form.Set("groupId", "a1")
	form.Set("inDrawer", "1")
	form.Set("values[0].label", "红")
	form.Set("values[0].key", "red")
	form.Add("values[0].enabled", "0")
	form.Add("values[0].enabled", "1")
	form.Set("values[1].label", "蓝")
	form.Add("values[1].enabled", "0")
	return form.Encode()
}

// ——— 分档 ———

// TestAttrGroupFormFailSplitsByRequestKind 属性组表单失败出口的分档判据。
func TestAttrGroupFormFailSplitsByRequestKind(t *testing.T) {
	h := &productPageHandle{}
	const msg = "属性组标识已被占用"

	// htmx 档：200 + 片段，且**不设** HX-Redirect（那是「跳走」的形态，与「原地留住输入」相反）。
	c, rec, cap := newAttrCaptureContext(t, "true", attrGroupSubmitBody())
	h.attrGroupFormFail(c, attrGroupModeCreate, "pr1", "new", msg)
	c.Writer.WriteHeaderNow() // gin 的状态码是延迟写的，不刷出来读到的永远是默认值
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx 档状态码 = %d，want 200（302 会被 htmx 跟随并吞掉 Location）", rec.Code)
	}
	if cap.name != attrGroupFormTemplate {
		t.Errorf("htmx 档渲染的片段 = %q，want %q", cap.name, attrGroupFormTemplate)
	}
	if loc := rec.Header().Get("HX-Redirect"); loc != "" {
		t.Errorf("htmx 失败档不该发 HX-Redirect（要原地重渲染片段），实际 %q", loc)
	}
	if _, ok := cap.data.(gin.H); !ok {
		t.Fatalf("片段 data 类型 = %T，want gin.H（片段的 FormEcho* 靠 map 键存在与否门控）", cap.data)
	}

	// 原生档：整页提示（200 + err 态），取代原先的 302 + ?err= 回列表。
	c2, rec2 := newProductJumpContext(t, "", "project=pr1", "projectId=pr1&name=颜色")
	h.attrGroupFormFail(c2, attrGroupModeCreate, "pr1", "new", msg)
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
	if !strings.Contains(body, "/admin/product-attributes?") || !strings.Contains(body, "project=pr1") {
		t.Fatalf("提示页回跳应回属性页并带上工程，body=%s", body)
	}
}

// TestAttrValuesFormFailSplitsByRequestKind 属性值表单失败出口的分档判据。
func TestAttrValuesFormFailSplitsByRequestKind(t *testing.T) {
	h := &productPageHandle{}

	c, rec, cap := newAttrCaptureContext(t, "true", attrValuesSubmitBody())
	h.attrValuesFormFail(c, "pr1", "a1", "属性值标识重复")
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx 档状态码 = %d，want 200", rec.Code)
	}
	if cap.name != attrValuesFormTemplate {
		t.Errorf("htmx 档渲染的片段 = %q，want %q", cap.name, attrValuesFormTemplate)
	}

	c2, rec2 := newProductJumpContext(t, "", "project=pr1", attrValuesSubmitBody())
	h.attrValuesFormFail(c2, "pr1", "a1", "属性值标识重复")
	c2.Writer.WriteHeaderNow()
	if rec2.Code != http.StatusOK {
		t.Fatalf("原生档状态码 = %d，want 200（提示页）", rec2.Code)
	}
	body := rec2.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("原生档应渲染 err 态提示页，body=%s", body)
	}
	if !strings.Contains(body, "/admin/product-attributes?") || !strings.Contains(body, "project=pr1") {
		t.Fatalf("提示页回跳应回属性页并带上工程，body=%s", body)
	}
}

// ——— 回填 data 的形状 ———

// TestAttrGroupFormFailEchoesSubmittedValues 属性组表单的回填内容。
func TestAttrGroupFormFailEchoesSubmittedValues(t *testing.T) {
	h := &productPageHandle{}
	c, _, cap := newAttrCaptureContext(t, "true", attrGroupSubmitBody())
	h.attrGroupFormFail(c, attrGroupModeCreate, "pr1", "new", "标识已被占用")

	data := capturedData(t, cap)

	// 键名必须带 FormEcho 前缀（页面键空间里已有同名键的风险见 util 注释）。
	if _, exists := data["Form"]; exists {
		t.Error("回填键不能叫 Form（与页面已有键撞名会让片段渲染失败）")
	}
	echo, ok := data["FormEcho"].(gin.H)
	if !ok {
		t.Fatalf("data[\"FormEcho\"] 类型 = %T，want gin.H", data["FormEcho"])
	}
	if got := echo["name"]; got != "颜色" {
		t.Errorf("FormEcho.name = %v，want 颜色", got)
	}
	if got := echo["sort"]; got != "3" {
		t.Errorf("FormEcho.sort = %v，want 3（数字字段也要回填）", got)
	}
	// 清单里列出的字段都必须存在（补零值）：模板读不到就整块渲染失败。
	for _, field := range attrGroupFormFields {
		if _, exists := echo[field]; !exists {
			t.Errorf("FormEcho 缺字段 %q —— 模板读不到会整块渲染失败（htmx 不 swap）", field)
		}
	}
	multi, ok := data["FormEchoMulti"].(gin.H)
	if !ok {
		t.Fatalf("data[\"FormEchoMulti\"] 类型 = %T，want gin.H", data["FormEchoMulti"])
	}
	// 勾选态必须能按值命中：隐藏域与复选框两个值都在，且保持提交顺序。
	if got, _ := multi["isVariation"].([]string); len(got) != 2 || got[0] != "0" || got[1] != "1" {
		t.Errorf("FormEchoMulti.isVariation = %v，want [0 1]（按值命中回填勾选态）", got)
	}

	// 结构性键：形态、地址、组 id、值行、抽屉申报、错误槽。
	if got := data["IsCreate"]; got != true {
		t.Errorf("IsCreate = %v，want true（新建形态才渲染值行编辑器）", got)
	}
	if got := data["Action"]; got != attrGroupCreateAction {
		t.Errorf("Action = %v，want %v", got, attrGroupCreateAction)
	}
	if got := data["GroupID"]; got != "new" {
		t.Errorf("GroupID = %v，want new（值编辑器容器 id 的依据）", got)
	}
	rows, ok := data["RowsCtx"].(attrRowsCtx)
	if !ok {
		t.Fatalf("data[\"RowsCtx\"] 类型 = %T，want attrRowsCtx", data["RowsCtx"])
	}
	if len(rows.Rows) != 1 {
		t.Fatalf("RowsCtx.Rows 行数 = %d，want 1（用户刚编的那一行要留住）", len(rows.Rows))
	}
	if rows.Rows[0].Label != "红" || rows.Rows[0].Key != "red" {
		t.Errorf("回填行 = %+v，want label=红 key=red", rows.Rows[0])
	}
	if got := data["InDrawer"]; got != true {
		t.Errorf("InDrawer = %v，want true（表单带了 inDrawer 隐藏域）", got)
	}
	if got := data["SubmitErr"]; got != "标识已被占用" {
		t.Errorf("SubmitErr = %v，want 标识已被占用", got)
	}
	// 片段取词靠数据里的 t 键（带参数 include，拿不到页面 data）：缺了它整块渲染失败。
	if _, exists := data["t"]; !exists {
		t.Error(`data 缺 t（片段里的 .["t"](...) 会在那一行失败）`)
	}
}

// TestAttrGroupFormFailKeepsDrawerShapeByDeclaration 抽屉形态由表单隐藏域申报。
func TestAttrGroupFormFailKeepsDrawerShapeByDeclaration(t *testing.T) {
	h := &productPageHandle{}

	// 表单申报了 inDrawer（抽屉里的正常提交）→ 片段要能渲染出「取消」按钮。
	c, _, cap := newAttrCaptureContext(t, "true", "name=颜色&inDrawer=1")
	h.attrGroupFormFail(c, attrGroupModeEdit, "pr1", "a1", "失败")
	if capturedData(t, cap)["InDrawer"] != true {
		t.Error("表单申报了 inDrawer，片段应带 InDrawer（否则抽屉里失败一次「取消」按钮就没了）")
	}

	// 没申报（片段被别处复用）→ 不该凭空渲染抽屉专属按钮。
	c2, _, cap2 := newAttrCaptureContext(t, "true", "name=颜色")
	h.attrGroupFormFail(c2, attrGroupModeEdit, "pr1", "a1", "失败")
	if _, exists := capturedData(t, cap2)["InDrawer"]; exists {
		t.Error("表单没申报 inDrawer 时不该给 InDrawer 键")
	}
}

// TestAttrGroupFormFailEditModePointsAtUpdate 编辑形态的地址与 id 回填。
func TestAttrGroupFormFailEditModePointsAtUpdate(t *testing.T) {
	h := &productPageHandle{}
	c, _, cap := newAttrCaptureContext(t, "true", "id=a1&name=颜色&key=color&sort=1&isVariation=0&inDrawer=1")
	h.attrGroupFormFail(c, attrGroupModeEdit, "pr1", "a1", "失败")

	data := capturedData(t, cap)
	if got := data["Action"]; got != attrGroupUpdateAction {
		t.Errorf("Action = %v，want %v（编辑抽屉必须打回 update）", got, attrGroupUpdateAction)
	}
	if got := data["IsCreate"]; got != false {
		t.Errorf("IsCreate = %v，want false（编辑形态不渲染值行编辑器）", got)
	}
	if got := data["GroupID"]; got != "a1" {
		t.Errorf("GroupID = %v，want a1", got)
	}
	if got, _ := data["FormEcho"].(gin.H)["id"]; got != "a1" {
		t.Errorf("FormEcho.id = %v，want a1", got)
	}
}

// TestAttrValuesFormFailEchoesValueRows 值表单的回填主体是行数据（不是单值）。
func TestAttrValuesFormFailEchoesValueRows(t *testing.T) {
	h := &productPageHandle{}
	c, _, cap := newAttrCaptureContext(t, "true", attrValuesSubmitBody())
	h.attrValuesFormFail(c, "pr1", "a1", "属性值标识重复")

	data := capturedData(t, cap)
	rows, ok := data["RowsCtx"].(attrRowsCtx)
	if !ok {
		t.Fatalf("data[\"RowsCtx\"] 类型 = %T，want attrRowsCtx", data["RowsCtx"])
	}
	if len(rows.Rows) != 2 {
		t.Fatalf("回填行数 = %d，want 2", len(rows.Rows))
	}
	if rows.Rows[0].Label != "红" || !rows.Rows[0].Enabled {
		t.Errorf("第 1 行 = %+v，want label=红 且启用（提交里有 1）", rows.Rows[0])
	}
	// 这一条钉的是「禁用也能存」：按「字段是否存在」判时它恒为启用（用户取消的勾选被静默丢弃）。
	if rows.Rows[1].Label != "蓝" || rows.Rows[1].Enabled {
		t.Errorf("第 2 行 = %+v，want label=蓝 且**未**启用（用户明确取消了勾选）", rows.Rows[1])
	}
	if got := data["Action"]; got != attrValuesAction {
		t.Errorf("Action = %v，want %v", got, attrValuesAction)
	}
	if got := data["InDrawer"]; got != true {
		t.Errorf("InDrawer = %v，want true", got)
	}
	if got, _ := data["FormEcho"].(gin.H)["id"]; got != "a1" {
		t.Errorf("FormEcho.id = %v，want a1", got)
	}
}

// 失败渲染回放原始控件值，落库仍用独立的归一化请求。
func TestAttrValuesFormFailPreservesSparseRawRows(t *testing.T) {
	form := url.Values{"projectId": {"pr1"}, "id": {"a1"}, "groupId": {"a1"}, "inDrawer": {"1"}}
	form.Set("values[0].label", "  红  ")
	form.Set("values[0].key", " red ")
	form.Set("values[0].sort", "oops")
	form.Add("values[0].enabled", "0")
	form.Add("values[0].enabled", "1")
	form.Set("values[2].label", "")
	form.Set("values[2].key", "")
	form.Set("values[2].sort", "  ")
	form.Set("values[2].enabled", "0")
	form.Set("values[5].label", " 蓝 ")
	form.Set("values[5].sort", "-03")
	form.Set("values[5].enabled", "0")
	c, _, cap := newAttrCaptureContext(t, "true", form.Encode())
	// 写入请求维持原语义：忽略中间空行。
	if got := attrRowsFromForm(c); len(got) != 2 || got[0].Label != "红" || got[0].Sort != 0 || got[1].Label != "蓝" || got[1].Sort != -3 {
		t.Fatalf("保存输入 = %+v，want 两条解析后的非空行", got)
	}
	(&productPageHandle{}).attrValuesFormFail(c, "pr1", "a1", "重复")
	data := capturedData(t, cap)
	rows := data["RowsCtx"].(attrRowsCtx).Rows
	if len(rows) != 3 || rows[0].Index != 0 || rows[0].Label != "  红  " || rows[0].Key != " red " || rows[0].Sort != "oops" || !rows[0].Enabled ||
		rows[1].Index != 2 || rows[1].Label != "" || rows[1].Sort != "  " || rows[1].Enabled ||
		rows[2].Index != 5 || rows[2].Label != " 蓝 " || rows[2].Sort != "-03" || rows[2].Enabled {
		t.Fatalf("失败回显行 = %+v，want 原始位置、空行、字符串及开关", rows)
	}
}

// TestAttrGroupFormFailPreservesRawValueRows 单独覆盖新建属性组的行回显。
func TestAttrGroupFormFailPreservesRawValueRows(t *testing.T) {
	form := url.Values{"projectId": {"pr1"}, "inDrawer": {"1"}}
	form.Set("values[0].label", " ")
	form.Set("values[0].sort", "")
	form.Set("values[3].label", " 蓝 ")
	form.Set("values[3].sort", "bad")
	c, _, cap := newAttrCaptureContext(t, "true", form.Encode())
	(&productPageHandle{}).attrGroupFormFail(c, attrGroupModeCreate, "pr1", "new", "重复")
	rows := capturedData(t, cap)["RowsCtx"].(attrRowsCtx).Rows
	if len(rows) != 2 || rows[0].Index != 0 || rows[0].Label != " " || rows[0].Sort != "" || rows[1].Index != 3 || rows[1].Label != " 蓝 " || rows[1].Sort != "bad" {
		t.Fatalf("新建属性组失败回显行 = %+v", rows)
	}
}

// TestAttrFailedFormJetRender 让三类失败出口经过真实 Jet 渲染器。
func TestAttrFailedFormJetRender(t *testing.T) {
	cases := []struct {
		name, template string
		withRows       bool
		fail           func(*gin.Context)
	}{
		{"新建组", attrGroupFormTemplate, true, func(c *gin.Context) {
			(&productPageHandle{}).attrGroupFormFail(c, attrGroupModeCreate, "pr1", "new", "重复")
		}},
		{"编辑组", attrGroupFormTemplate, false, func(c *gin.Context) {
			(&productPageHandle{}).attrGroupFormFail(c, attrGroupModeEdit, "pr1", "a1", "重复")
		}},
		{"属性值", attrValuesFormTemplate, true, func(c *gin.Context) {
			(&productPageHandle{}).attrValuesFormFail(c, "pr1", "a1", "重复")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
			engine.POST("/submit", tc.fail)
			form := url.Values{"csrf_token": {"tok-1"}, "projectId": {"pr1"}, "id": {"a1"}, "groupId": {"a1"}, "inDrawer": {"1"}, "name": {"颜色"}}
			form.Set("values[0].label", " 红 ")
			form.Set("values[0].sort", "oops")
			form.Set("values[1].label", "")
			form.Set("values[1].sort", "")
			form.Set("values[2].label", " 蓝 ")
			form.Set("values[2].sort", "-03")
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("HX-Request", "true")
			engine.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "页面暂时无法显示") {
				t.Fatalf("%s 真 Jet 响应状态=%d 内容=%s", tc.template, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, `role="alert"`) {
				t.Error("响应缺失错误槽")
			}
			if tc.withRows {
				for _, want := range []string{`name="values[0].sort" value="oops"`, `name="values[1].label" value=""`, `name="values[2].sort" value="-03"`} {
					if !strings.Contains(body, want) {
						t.Errorf("响应缺失 %s", want)
					}
				}
				if strings.Contains(body, `name="values[1].sort" value="0"`) {
					t.Error("空排序值被归一成 0")
				}
			}
		})
	}
}

// attributeRespForTest 一行属性组数据（attrRowDrawerForms 的入参形状）。
func attributeRespForTest(id string) *productdto.AttributeResp {
	return &productdto.AttributeResp{
		ID: id, Name: "颜色", Key: "color", IsVariation: true, Sort: 0,
		Values: []productdto.AttributeValueResp{
			{ID: "av1", Key: "red", Label: "红", Sort: 0, Enabled: true},
		},
	}
}

// capturedData 取出捕获到的片段 data（gin.H）。
func capturedData(t *testing.T, cap *captureRender) gin.H {
	t.Helper()
	data, ok := cap.data.(gin.H)
	if !ok {
		t.Fatalf("片段 data 类型 = %T，want gin.H", cap.data)
	}
	return data
}

// TestAttrFormDataShapeMatchesFirstPaint 首屏与失败重渲染的数据形状必须一致。
//
// 这是本批最容易分叉的地方：页面首屏由 attrCreateDrawerForm / attrRowDrawerForms 组装，
// 失败重渲染由 attrGroupFormFail / attrValuesFormFail 组装 —— 两处少一个键的表现不是
// 「少渲一块」，而是整块渲染失败（500），而 htmx 对 5xx 不 swap：用户点保存后什么都看不到。
func TestAttrFormDataShapeMatchesFirstPaint(t *testing.T) {
	h := &productPageHandle{}
	tr := func(_, fallback string) string { return fallback }
	// 失败档专属键：它们本就不该出现在首屏。
	failOnly := map[string]bool{
		"FormEcho": true, "FormEchoChecked": true, "FormEchoMulti": true, "SubmitErr": true,
	}
	// 壳层键（lang / t / csrf_token / PermSet / 导航…）：失败档的 data 会过一遍 shell.Prepare，
	// 这些键不是抽屉的数据 —— 用一个空 Prepare 现场取键集，不硬编码清单（会漂移）。
	shellKeys := map[string]bool{}
	probe, _, _ := newAttrCaptureContext(t, "", "")
	for k := range shell.Prepare(probe, gin.H{}) {
		shellKeys[k] = true
	}
	compare := func(t *testing.T, label string, first, fail gin.H) {
		t.Helper()
		for key := range first {
			if _, ok := fail[key]; !ok {
				t.Errorf("%s：失败重渲染缺首屏键 %q —— 两条路径形状分叉，片段会整块渲染失败", label, key)
			}
		}
		for key := range fail {
			if failOnly[key] || shellKeys[key] {
				continue
			}
			if _, ok := first[key]; !ok {
				t.Errorf("%s：失败重渲染多出首屏没有的键 %q（首屏那条渲染路径会取不到它）", label, key)
			}
		}
	}

	// 属性组：新建形态（首屏 attrCreateDrawerForm ↔ 失败 attrGroupFormFail(create)）。
	c, _, cap := newAttrCaptureContext(t, "true", attrGroupSubmitBody())
	h.attrGroupFormFail(c, attrGroupModeCreate, "pr1", "new", "失败")
	compare(t, "属性组/新建", attrCreateDrawerForm("tok", "pr1", "", tr), capturedData(t, cap))

	// 属性组：编辑形态（首屏 attrRowDrawerForms ↔ 失败 attrGroupFormFail(edit)）。
	c2, _, cap2 := newAttrCaptureContext(t, "true", "id=a1&name=颜色&key=color&inDrawer=1")
	h.attrGroupFormFail(c2, attrGroupModeEdit, "pr1", "a1", "失败")
	editFirst, _ := attrRowDrawerForms("tok", "pr1", "", tr, attributeRespForTest("a1"))
	compare(t, "属性组/编辑", editFirst, capturedData(t, cap2))

	// 属性值（首屏 attrValuesFormData ↔ 失败 attrValuesFormFail）。
	c3, _, cap3 := newAttrCaptureContext(t, "true", attrValuesSubmitBody())
	h.attrValuesFormFail(c3, "pr1", "a1", "失败")
	_, valuesFirst := attrRowDrawerForms("tok", "pr1", "", tr, attributeRespForTest("a1"))
	compare(t, "属性值", valuesFirst, capturedData(t, cap3))
}

// ——— 勾选态判据 ———

// TestFormValueHasIgnoresHiddenDefault 勾选态判据（同名隐藏域打底的坑）。
//
// 这条钉的是一个**已修的真实缺陷**：属性页的勾选字段是「hidden 0 打底 + checkbox 1」，
// 浏览器提交 ["0","1"]；按「取第一个值」（c.PostForm）或「字段是否存在」判都得到相反结论 ——
// 「参与变体」永远存成 false、属性值的「启用」永远存成 true，页面照样 200、不报任何错。
func TestFormValueHasIgnoresHiddenDefault(t *testing.T) {
	checked := url.Values{"isVariation": {"0", "1"}}
	if !formValueHas(checked, "isVariation", "1") {
		t.Error("提交里存在 1 时应判为勾选（隐藏域的 0 在前）")
	}
	unchecked := url.Values{"isVariation": {"0"}}
	if formValueHas(unchecked, "isVariation", "1") {
		t.Error("只提交了隐藏域（0）时应判为未勾选")
	}
	if formValueHas(url.Values{}, "isVariation", "1") {
		t.Error("字段完全缺失时应判为未勾选")
	}

	// values[n].enabled 的「缺省即启用」语义：字段没给 → nil（交 service 兜底为启用），
	// 明确给了 0 → false（用户的取消必须生效）。
	if got := attrFormOptionalChecked(url.Values{}, "values[0].enabled"); got != nil {
		t.Errorf("字段缺失应给 nil（service 兜底为启用），实际 %v", *got)
	}
	if got := attrFormOptionalChecked(unchecked, "isVariation"); got == nil || *got {
		t.Errorf("明确提交了 0 应给 false，实际 %v", got)
	}
}

// TestAttrFormCheckedReadsAllSubmittedValues 走 gin 上下文的勾选判据（handler 用的那一个）。
func TestAttrFormCheckedReadsAllSubmittedValues(t *testing.T) {
	c, _, _ := newAttrCaptureContext(t, "true", attrGroupSubmitBody())
	if !attrFormChecked(c, "isVariation") {
		t.Error("提交里存在 isVariation=1 时应判为勾选（c.PostForm 只看第一个值，会得到 0）")
	}
	if attrFormChecked(c, "trackQuantity") {
		t.Error("没提交的字段不该判为勾选")
	}
}

// ——— 字段清单 ↔ 模板 双向断言 ———

// TestAttrFormFieldsMatchTemplate 字段清单与模板**双向**对齐。
//
// 正向：模板里读到的每个 `FormEcho*.字段` 都必须在清单里 —— 漏列让片段渲染失败 → 500 →
//
//	htmx 不 swap（用户点保存后什么都看不到）；
//
// 反向：清单里列的每个字段模板都得真的在读 —— 只增不减的清单会掩盖「模板删了字段、清单还留着」，
//
//	而那种残留下次会被当成「已经在读了」的依据。
func TestAttrFormFieldsMatchTemplate(t *testing.T) {
	cases := []struct {
		name   string
		tmpl   string
		fields []string
	}{
		{"属性组表单", attrGroupFormTemplate, attrGroupFormFields},
		{"属性值表单", attrValuesFormTemplate, attrValuesFormFields},
	}
	// 模板在 internal/templates 下（本包在 internal/module/product/inbound/http）。
	elemRe := regexp.MustCompile(`FormEcho(?:Checked|Multi)?\.([A-Za-z0-9_]+)`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "templates", tc.tmpl))
			if err != nil {
				t.Fatalf("读片段模板失败: %v", err)
			}
			inTemplate := map[string]bool{}
			for _, m := range elemRe.FindAllStringSubmatch(string(src), -1) {
				inTemplate[m[1]] = true
			}
			declared := map[string]bool{}
			for _, f := range tc.fields {
				declared[f] = true
			}
			for field := range inTemplate {
				if !declared[field] {
					t.Errorf("模板读了 FormEcho*.%s，但清单里没有它 —— 失败片段取不到这个键，整块渲染失败", field)
				}
			}
			for field := range declared {
				if !inTemplate[field] {
					t.Errorf("清单里的 %s 模板没有在读 —— 清单残留（模板改了名，清单没跟）", field)
				}
			}
			// 清单本身不能有重复项：重复不会报错，只会让「有没有列全」的判据失真。
			seen := map[string]bool{}
			for _, f := range tc.fields {
				if seen[f] {
					t.Errorf("清单里有重复字段 %q", f)
				}
				seen[f] = true
			}
		})
	}
}
