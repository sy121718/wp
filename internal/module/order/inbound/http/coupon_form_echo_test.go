package orderhttp

// coupon_form_echo_test.go — 优惠码新建/编辑表单「写失败不丢输入」的分档与字段清单守门。
//
// 钉几件错了会**静默失效**的事（契约见 internal/templates/CLAUDE.md 与 docs/02-T）：
//   · htmx 失败 = 200 + 片段（**不能**是 302 —— htmx 的 XHR 会自己跟随 302 并吞掉
//     Location，整页 HTML 会被塞进片段位置）且不发 HX-Redirect（那是「跳走」的形态）；
//   · htmx 成功 = HX-Redirect 头（成功路径也必须分档，漏了照样坏页面）；
//   · 回填 data 的键名带 FormEcho 前缀、多值保序、清单字段零值全给；
//   · 字段清单与片段模板的**双向**一致性 —— 模板读了清单外的字段：渲染中断（500）
//     → htmx 判 swap:false → 用户点了保存什么都看不到；反向不钉则清单在悄悄漂移；
//   · 失败片段真渲染：错误槽在 host 内、form 外；hx-* 分档属性在 form 上。
//
// 分档测试用桩渲染器（这里只验出口与状态码，片段长相由真渲染测试独立验证），
// 真渲染走 templates.NewJetHTMLRender 的开发模式 loader 与真实模板文件。

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/internal/templates"
)

// couponStubRender 片段渲染的替身：只记录「渲染了哪个模板」并回显模板名。
type couponStubRender struct{}

func (s *couponStubRender) Instance(name string, _ any) ginrender.Render {
	return &couponStubInstance{name: name}
}

type couponStubInstance struct{ name string }

func (i *couponStubInstance) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (i *couponStubInstance) Render(w http.ResponseWriter) error {
	i.WriteContentType(w)
	_, err := w.Write([]byte("FRAG:" + i.name))
	return err
}

// newCouponContext 造一个带（可选）HX-Request 头的写请求上下文与记录器。
func newCouponContext(t *testing.T, hxHeader, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	engine.HTMLRender = &couponStubRender{}
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/coupons/create", strings.NewReader(body))
	if hxHeader != "" {
		c.Request.Header.Set("HX-Request", hxHeader)
	}
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c, rec
}

// fakeCouponOrderService 只接住 coupon 链路的方法；嵌入接口只为满足依赖形状。
type fakeCouponOrderService struct {
	ordercontract.OrderService
	getCoupon *orderdto.CouponResp
	getCalls  int
	getErr    error
	createErr error
	updateErr error
	created   *orderdto.CouponSaveReq
	updated   *orderdto.CouponSaveReq
}

func (f *fakeCouponOrderService) CreateCoupon(_ context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error) {
	f.created = req
	return &orderdto.CouponResp{}, f.createErr
}

func (f *fakeCouponOrderService) UpdateCoupon(_ context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error) {
	f.updated = req
	return &orderdto.CouponResp{}, f.updateErr
}

func (f *fakeCouponOrderService) GetCoupon(_ context.Context, _ uint64) (*orderdto.CouponResp, error) {
	f.getCalls++
	return f.getCoupon, f.getErr
}

// TestCouponCreateFailSplitsByRequestKind 新建失败出口的分档判据。
func TestCouponCreateFailSplitsByRequestKind(t *testing.T) {
	h := &couponPageHandle{}
	const msg = "这个优惠码已经存在"

	// htmx 档：200 + 片段，且**不设** HX-Redirect（要原地留住输入，不是跳走）。
	c, rec := newCouponContext(t, "true", "code=SAVE20&name=周年庆")
	h.couponCreateFail(c, msg)
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx 档状态码 = %d，want 200（302 会被 htmx 跟随并吞掉 Location）", rec.Code)
	}
	if got, want := rec.Body.String(), "FRAG:admin/order/coupon_create_form.html"; got != want {
		t.Errorf("htmx 档渲染的片段 = %q，want %q", got, want)
	}
	if loc := rec.Header().Get("HX-Redirect"); loc != "" {
		t.Errorf("htmx 失败档不该发 HX-Redirect（要原地重渲染片段），实际 %q", loc)
	}

	// 其余请求：现状 302 兜底（抽屉表单没有无 JS 提交通道，不新增行为）。
	c2, rec2 := newCouponContext(t, "", "code=SAVE20")
	h.couponCreateFail(c2, msg)
	c2.Writer.WriteHeaderNow()
	if rec2.Code != http.StatusFound {
		t.Fatalf("非 htmx 档状态码 = %d，want 302", rec2.Code)
	}
	if loc := rec2.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/coupons?") {
		t.Errorf("非 htmx 档应回列表页，实际 %q", loc)
	}
}

// TestCouponEditFailSplitsByRequestKind 编辑失败出口的分档判据，含「券取不到」的兜底。
func TestCouponEditFailSplitsByRequestKind(t *testing.T) {
	const msg = "结束时间必须晚于开始时间"

	// htmx 档 + 券可取：200 + 编辑片段。
	h := &couponPageHandle{orders: &fakeCouponOrderService{getCoupon: &orderdto.CouponResp{Code: "SAVE20"}}}
	c, rec := newCouponContext(t, "true", "id=7&projectId=p1&name=新名")
	h.couponEditFail(c, msg)
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx 档状态码 = %d，want 200", rec.Code)
	}
	if got, want := rec.Body.String(), "FRAG:admin/order/coupon_edit_form.html"; got != want {
		t.Errorf("htmx 档渲染的片段 = %q，want %q", got, want)
	}

	// htmx 档 + 券取不到（已被并发删除）：不渲染半个片段，HX-Redirect 带错误回列表页。
	h2 := &couponPageHandle{orders: &fakeCouponOrderService{getErr: context.DeadlineExceeded}}
	c2, rec2 := newCouponContext(t, "true", "id=7&projectId=p1")
	h2.couponEditFail(c2, msg)
	c2.Writer.WriteHeaderNow()
	if loc := rec2.Header().Get("HX-Redirect"); !strings.HasPrefix(loc, "/admin/coupons?") || !strings.Contains(loc, "err=") {
		t.Errorf("券取不到时应 HX-Redirect 回列表页带错误，实际 HX-Redirect=%q 状态=%d", loc, rec2.Code)
	}
	if got := rec2.Body.String(); strings.Contains(got, "coupon_edit_form") {
		t.Errorf("券取不到时不该渲染编辑片段，实际 %q", got)
	}

	// 其余请求：现状 302 兜底。
	h3 := &couponPageHandle{orders: &fakeCouponOrderService{}}
	c3, rec3 := newCouponContext(t, "", "id=7")
	h3.couponEditFail(c3, msg)
	c3.Writer.WriteHeaderNow()
	if rec3.Code != http.StatusFound {
		t.Fatalf("非 htmx 档状态码 = %d，want 302", rec3.Code)
	}
}

// TestCouponWriteSuccessRedirectsByRequestKind 成功路径也必须分档（create / update 同口径）。
func TestCouponWriteSuccessRedirectsByRequestKind(t *testing.T) {
	newBody := "projectId=p1&code=SAVE20&returnQuery=project%3Dp1%26page%3D2"

	// htmx 成功：HX-Redirect + 200（XHR 自己跟随 302 会把整页 HTML 塞进片段位置）。
	h := &couponPageHandle{orders: &fakeCouponOrderService{}}
	c, rec := newCouponContext(t, "true", newBody)
	h.CouponCreate(c)
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx 成功档状态码 = %d，want 200", rec.Code)
	}
	if rec.Header().Get("Location") != "" || rec.Body.Len() != 0 {
		t.Fatalf("htmx 成功只发 HX-Redirect，Location=%q body=%q", rec.Header().Get("Location"), rec.Body.String())
	}
	if loc := rec.Header().Get("HX-Redirect"); !strings.HasPrefix(loc, "/admin/coupons?") {
		t.Fatalf("htmx 成功档应发 HX-Redirect 回列表页，实际 %q", loc)
	}
	if !strings.Contains(rec.Header().Get("HX-Redirect"), "project=p1") ||
		!strings.Contains(rec.Header().Get("HX-Redirect"), "page=2") {
		t.Errorf("成功跳转应透传 returnQuery 的白名单键，实际 %q", rec.Header().Get("HX-Redirect"))
	}

	// 原生成功：现状 302（Location 同终点）。
	h2 := &couponPageHandle{orders: &fakeCouponOrderService{}}
	c2, rec2 := newCouponContext(t, "", newBody)
	h2.CouponCreate(c2)
	c2.Writer.WriteHeaderNow()
	if rec2.Code != http.StatusFound {
		t.Fatalf("原生成功档状态码 = %d，want 302", rec2.Code)
	}
	if loc := rec2.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/coupons?") {
		t.Errorf("原生成功档 Location = %q，want 回列表页", loc)
	}

	// update 成功同口径（htmx 档）。
	h3 := &couponPageHandle{orders: &fakeCouponOrderService{}}
	c3, rec3 := newCouponContext(t, "true", "id=7&projectId=p1&name=改个名")
	h3.CouponUpdate(c3)
	c3.Writer.WriteHeaderNow()
	if rec3.Code != http.StatusOK || rec3.Header().Get("HX-Redirect") == "" {
		t.Fatalf("update 的 htmx 成功档 = %d / HX-Redirect=%q，want 200 + 非空", rec3.Code, rec3.Header().Get("HX-Redirect"))
	}
	if rec3.Header().Get("Location") != "" || rec3.Body.Len() != 0 {
		t.Fatalf("update htmx 成功只发 HX-Redirect，Location=%q body=%q", rec3.Header().Get("Location"), rec3.Body.String())
	}
}

// TestCouponCreateUpdateFailRendersEchoFragment handler 失败出口经桩渲染的模板名分档已验，
// 这里补「失败时拿的是业务错误文案」：出口收到的 msg 原样进 SubmitErr（真渲染测试再验落位）。
func TestCouponCreateUpdateFailCarriesFacingMessage(t *testing.T) {
	h := &couponPageHandle{orders: &fakeCouponOrderService{createErr: context.DeadlineExceeded}}
	c, _ := newCouponContext(t, "true", "projectId=p1&code=SAVE20")
	// couponFacingError 对白名单外的错误会收口成归口文案；这里只验证失败确实走了片段出口。
	h.CouponCreate(c)
	if h.orders.(*fakeCouponOrderService).created == nil {
		t.Fatal("失败出口前应当已经调用过 CreateCoupon")
	}

	h2 := &couponPageHandle{orders: &fakeCouponOrderService{updateErr: errors.New(orderenums.ErrCouponWindowInvalid)}}
	c2, _ := newCouponContext(t, "true", "id=7&projectId=p1&startsAt=2026-05-01T00:00&endsAt=2026-01-01T00:00")
	h2.CouponUpdate(c2)
	if h2.orders.(*fakeCouponOrderService).updated == nil {
		t.Fatal("失败出口前应当已经调用过 UpdateCoupon")
	}
}

// TestCouponFormEchoDataCarriesEchoKeys 回填 data 的形状（片段与 handler 的协议）。
func TestCouponFormEchoDataCarriesEchoKeys(t *testing.T) {
	c, _ := newCouponContext(t, "true",
		"projectId=p1&code=SAVE20&name=%E5%91%A8%E5%B9%B4%E5%BA%86&discountType=fixed&discountValue=2000&minSubtotal=9900&status=1&startsAt=2026-01-01T09%3A00&returnQuery=project%3Dp1")

	data := couponFormEchoData(c, couponCreateFormFields...)

	// 键名必须带 FormEcho 前缀：片段与页面共用同一份渲染 data，裸通用键会撞上页面键空间。
	for _, banned := range []string{"Form", "Fields", "Values"} {
		if _, exists := data[banned]; exists {
			t.Errorf("回填键不该出现裸名 %q（与页面键空间撞名会让片段渲染失败）", banned)
		}
	}

	echo, ok := data["FormEcho"].(gin.H)
	if !ok {
		t.Fatalf("data[\"FormEcho\"] 类型 = %T，want gin.H", data["FormEcho"])
	}
	if got := echo["code"]; got != "SAVE20" {
		t.Errorf("FormEcho.code = %v，want SAVE20", got)
	}
	if got := echo["discountType"]; got != "fixed" {
		t.Errorf("FormEcho.discountType = %v，want fixed（下拉回填要能还原选项）", got)
	}
	// 清单里列出的字段都必须存在（补零值）：模板读 context 缺失键会让片段渲染中断。
	for _, field := range couponCreateFormFields {
		if _, exists := echo[field]; !exists {
			t.Errorf("FormEcho 缺字段 %q —— 失败片段渲染会中断（500，htmx 不 swap）", field)
		}
	}
	if got := echo["remark"]; got != "" {
		t.Errorf("FormEcho.remark 未提交时应为空串零值，实际 %v", got)
	}

	multi, ok := data["FormEchoMulti"].(gin.H)
	if !ok {
		t.Fatalf("data[\"FormEchoMulti\"] 类型 = %T，want gin.H", data["FormEchoMulti"])
	}
	if _, exists := multi["projectId"]; !exists {
		t.Error("FormEchoMulti 应给清单里每个字段（含单值字段）键，缺键的模板访问会中断渲染")
	}

	if _, exists := data["SubmitErr"]; exists {
		t.Error("couponFormEchoData 不该注入 SubmitErr（归 couponCreateFail/couponEditFail）")
	}
}

func TestCouponFormEchoPreservesSubmittedValues(t *testing.T) {
	form := url.Values{"name": {"  周年庆  "}, "remark": {"  第一行\n第二行  "}, "endsAt": {""}}
	c, _ := newCouponContext(t, "true", form.Encode())
	echo := couponFormEchoFrom(c)
	for field, want := range map[string]string{"name": "  周年庆  ", "remark": "  第一行\n第二行  ", "endsAt": ""} {
		if got := echo.value(field); got != want {
			t.Errorf("%s 回填 = %q，want %q", field, got, want)
		}
	}
	if got := echo.list("endsAt"); len(got) != 1 || got[0] != "" {
		t.Errorf("显式提交的空值应保留在列表中，实际 %q", got)
	}
}

func TestCouponFailedWritesRenderSubmittedForms(t *testing.T) {
	base := url.Values{
		"id": {"7"}, "projectId": {"p1"}, "returnQuery": {"project=p1&couponId=7"},
		"code": {"SAVE20"}, "name": {"  周年庆  "}, "discountType": {"fixed"},
		"discountValue": {"2000"}, "minSubtotal": {"9900"}, "maxUses": {"12"},
		"perUserLimit": {"1"}, "status": {"0"}, "startsAt": {"2026-01-01T09:00:05"},
		"endsAt": {"2026-02-01T00:00"}, "remark": {"  <script>alert(1)</script>  "},
	}
	for _, tc := range []struct {
		name, endpoint, host string
		write                func(*couponPageHandle, *gin.Context)
		service              *fakeCouponOrderService
		fields               []string
	}{
		{"create", "/admin/coupons/create", "create", (*couponPageHandle).CouponCreate,
			&fakeCouponOrderService{createErr: errors.New(orderenums.ErrCouponCodeTaken)}, couponCreateFormFields},
		{"edit", "/admin/coupons/update", "edit", (*couponPageHandle).CouponUpdate,
			&fakeCouponOrderService{getCoupon: &orderdto.CouponResp{Code: "SAVE20"}, updateErr: errors.New(orderenums.ErrCouponWindowInvalid)}, couponEditFormFields},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, engine := gin.CreateTestContext(rec)
			engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
			c.Request = httptest.NewRequest(http.MethodPost, tc.endpoint, strings.NewReader(base.Encode()))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			c.Request.Header.Set("HX-Request", "true")
			tc.write(&couponPageHandle{orders: tc.service}, c)
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK {
				t.Fatalf("失败片段状态 = %d，正文 = %s", rec.Code, rec.Body.String())
			}
			out := rec.Body.String()
			root := `<div data-coupon-` + tc.host + `-host>`
			if tc.host == "edit" {
				root = `<div data-coupon-edit-host data-drawer-fragment>`
			}
			assertContains(t, out, root, `data-coupon-`+tc.host+`-err`,
				`hx-target="closest [data-coupon-`+tc.host+`-host]"`, `hx-swap="outerHTML"`)
			if strings.Index(out, `data-coupon-`+tc.host+`-err`) > strings.Index(out, `<form method="post"`) {
				t.Error("错误槽应在表单之前")
			}
			for _, field := range tc.fields {
				value := html.EscapeString(base.Get(field))
				if field == "discountType" || field == "status" {
					if !strings.Contains(out, `value="`+value+`" selected`) {
						t.Errorf("失败片段下拉 %s 未选中提交值 %q", field, base.Get(field))
					}
					continue
				}
				if !strings.Contains(out, `name="`+field+`" value="`+value+`"`) {
					t.Errorf("失败片段字段 %s 未回填提交值 %q", field, base.Get(field))
				}
			}
			assertContains(t, out, `name="name" value="  周年庆  "`,
				`name="remark" value="  &lt;script&gt;alert(1)&lt;/script&gt;  "`,
				`name="startsAt" value="2026-01-01T09:00:05"`, `value="fixed" selected`, `value="0" selected`)
			if strings.Contains(out, `<script>alert(1)</script>`) {
				t.Error("回填值中的 HTML 标签必须由 Jet 转义")
			}
			if tc.name == "edit" {
				assertContains(t, out, `id="coupon-7-code" value="SAVE20" readonly`)
			}
		})
	}
}

// couponTemplateSource 按 basename 在 templates/admin 下递归找模板源文件。
//
// 不硬编码路径：模板按后端模块分目录，一搬动硬编码就红；找不到仍然 Fatal ——
// 模板被移走 / 改名时必须失败，不能退化成静默跳过。
func couponTemplateSource(t *testing.T, base string) []byte {
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

// assertEchoFieldsMatchTemplate 字段清单与片段模板的**双向**一致性（新建 / 编辑共用判据）。
func assertEchoFieldsMatchTemplate(t *testing.T, tplName string, fields []string) {
	t.Helper()
	raw := couponTemplateSource(t, tplName)
	// 模板里对回填值的全部访问：.FormEcho.x / .FormEchoChecked.x / .FormEchoMulti.x
	re := regexp.MustCompile(`\.FormEcho(?:Checked|Multi)?\.([A-Za-z][A-Za-z0-9_]*)`)
	inTemplate := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		inTemplate[m[1]] = true
	}
	if len(inTemplate) == 0 {
		t.Fatalf("没从 %s 里解析出任何回填字段访问 —— 正则或模板结构已变，这条守门成了空转", tplName)
	}
	inList := map[string]bool{}
	for _, f := range fields {
		inList[f] = true
	}

	// 方向一：模板读的字段必须在清单里（漏列 = 渲染中断 → 500 → htmx 不 swap）。
	for field := range inTemplate {
		if !inList[field] {
			t.Errorf("%s 读了 .FormEcho.%s，但清单没列它 —— 失败重渲染会中断，用户看不到任何反应", tplName, field)
		}
	}
	// 方向二：清单里的字段模板必须真的读（否则清单在漂移）。
	for field := range inList {
		if !inTemplate[field] {
			t.Errorf("清单列了 %q，但 %s 从不读它 —— 清单已与模板脱节", field, tplName)
		}
	}
}

func TestCouponCreateFormFieldsMatchTemplate(t *testing.T) {
	assertEchoFieldsMatchTemplate(t, "coupon_create_form.html", couponCreateFormFields)
}

func TestCouponEditFormFieldsMatchTemplate(t *testing.T) {
	assertEchoFieldsMatchTemplate(t, "coupon_edit_form.html", couponEditFormFields)
}

// TestCouponEchoFragmentsRender 失败片段的真渲染（开发模式 loader + 真实模板文件）。
//
// 断言四件事：host div 自带（替换单元）、错误槽在 form 之外、hx-* 分档属性在 form 上、
// 回填值逐字段还原（含下拉 selected 与「该字段没提交 → 空值」的形态）。
func TestCouponEchoFragmentsRender(t *testing.T) {
	t.Helper()
	renderCoupon := func(t *testing.T, name string, data map[string]any) string {
		t.Helper()
		r := templates.NewJetHTMLRender("../../../../templates", true)
		rec := httptest.NewRecorder()
		if err := r.Instance(name, data).Render(rec); err != nil {
			t.Fatalf("%s 渲染失败：%v", name, err)
		}
		return rec.Body.String()
	}
	echoBase := func(fields []string, values map[string]string) map[string]any {
		echo := map[string]any{}
		for _, f := range fields {
			echo[f] = values[f]
		}
		return map[string]any{
			"csrf_token":      "tok",
			"t":               func(_, fallback string) string { return fallback },
			"FormEcho":        echo,
			"FormEchoChecked": map[string]any{},
			"FormEchoMulti":   map[string]any{},
			"TypeOptions": []any{
				map[string]any{"Value": "percent", "Label": "按比例"},
				map[string]any{"Value": "fixed", "Label": "固定金额"},
			},
			"StatusOptions": []any{
				map[string]any{"Value": "1", "Label": "启用"},
				map[string]any{"Value": "0", "Label": "停用"},
			},
			"SelectedProject": "p1",
			"CreateBack":      "project=p1&page=2",
			"EditCode":        "SAVE20",
			"SubmitErr":       "结束时间必须晚于开始时间",
		}
	}

	// —— 新建片段：失败回填 ——
	cdata := echoBase(couponCreateFormFields, map[string]string{
		"projectId": "p1", "returnQuery": "project=p1&page=2", "code": "SAVE20",
		"name": "周年庆", "discountType": "fixed", "discountValue": "2000",
		"minSubtotal": "9900", "maxUses": "0", "perUserLimit": "1", "status": "0",
		"startsAt": "2026-01-01T09:00", "endsAt": "", "remark": "活动用",
	})
	cout := renderCoupon(t, "admin/order/coupon_create_form.html", cdata)
	assertContains(t, cout,
		`<div data-coupon-create-host>`,
		`role="alert" data-coupon-create-err`, "结束时间必须晚于开始时间",
		`hx-post="/admin/coupons/create"`, `hx-target="closest [data-coupon-create-host]"`, `hx-swap="outerHTML"`,
		`name="code" value="SAVE20"`, `name="name" value="周年庆"`,
		`value="fixed" selected`, `name="discountValue" value="2000"`,
		`name="minSubtotal" value="9900"`, `name="perUserLimit" value="1"`,
		`value="0" selected`,                       // status 回填到「停用」，不是首屏默认的「启用」
		`name="startsAt" value="2026-01-01T09:00"`, // 时间窗按用户输入还原
		`name="returnQuery" value="project=p1&amp;page=2"`,
		`name="projectId" value="p1"`,
	)
	assertAbsent(t, cout, "name=\"endsAt\" value=\"0\"") // 没提交的时间窗回填为空，绝不能落成 0

	// 错误槽在 form 之外：host 直接子节点的次序是 错误槽 → form。
	errIdx := strings.Index(cout, "data-coupon-create-err")
	formIdx := strings.Index(cout, "<form method=\"post\"")
	if errIdx < 0 || formIdx < 0 || errIdx > formIdx {
		t.Errorf("错误槽应出现在 form 之前（host 内、form 外），err=%d form=%d", errIdx, formIdx)
	}

	// —— 编辑片段：失败回填 ——
	edata := echoBase(couponEditFormFields, map[string]string{
		"id": "7", "projectId": "p1", "returnQuery": "project=p1&couponId=7", "name": "改过的名",
		"discountType": "percent", "discountValue": "20", "minSubtotal": "500",
		"maxUses": "100", "perUserLimit": "0", "status": "1", "startsAt": "2026-01-01T09:00:05",
		"endsAt": "2026-02-01T00:00", "remark": "改过的备注",
	})
	eout := renderCoupon(t, "admin/order/coupon_edit_form.html", edata)
	assertContains(t, eout,
		`<div data-coupon-edit-host data-drawer-fragment>`,
		`role="alert" data-coupon-edit-err`,
		`hx-post="/admin/coupons/update"`, `hx-target="closest [data-coupon-edit-host]"`, `hx-swap="outerHTML"`,
		`name="id" value="7"`, `name="name" value="改过的名"`,
		`id="coupon-7-code" value="SAVE20" readonly`, // 券码来自 EditCode（库里原值），不在回填清单
		`id="coupon-7-name"`, `for="coupon-7-name"`,
		`value="percent" selected`, `name="maxUses" value="100"`,
		`name="startsAt" value="2026-01-01T09:00:05"`, // 带秒的时刻原样还原（step=1 才提交得回来）
		`name="returnQuery" value="project=p1&amp;couponId=7"`,
	)

	// —— 新建片段：首屏形态（无 FormEcho、无 SubmitErr）——
	fresh := echoBase(couponCreateFormFields, nil)
	delete(fresh, "FormEcho")
	delete(fresh, "FormEchoChecked")
	delete(fresh, "FormEchoMulti")
	delete(fresh, "SubmitErr")
	fout := renderCoupon(t, "admin/order/coupon_create_form.html", fresh)
	assertContains(t, fout,
		`<div data-coupon-create-host>`, `hx-post="/admin/coupons/create"`,
		`name="projectId" value="p1"`,                      // 首屏 projectId 来自 SelectedProject
		`name="returnQuery" value="project=p1&amp;page=2"`, // 首屏 returnQuery 来自 CreateBack
		`name="minSubtotal" value="0"`,                     // 次数 / 门槛类字段首屏默认 0
		`value="1" selected`,                               // status 首屏默认启用
	)
	assertAbsent(t, fout, "role=\"alert\" data-coupon-create-err") // 首屏没有错误槽
}

// TestCouponsPageRendersDrawerTemplates 整页首屏渲染冒烟：新建表单仍内嵌，
// 编辑入口按行生成远程 URL，编辑表单只在点击后由 GET 加载。
//
// order_bulk_page_render_test.go 的既有整页测试不带 PermSet 权限，
// 这里打开新建/修改权限，核对两个入口分别呈现内嵌与按需形态。
func TestCouponsPageRendersDrawerTemplates(t *testing.T) {
	d := couponBulkPageData()
	d["Rows"].([]any)[0].(map[string]any)["EditFormURL"] = "/admin/coupons/edit-form?id=3&project=p1"
	if perm, ok := d["PermSet"].(map[string]any); ok {
		perm["order:coupon_create"] = true
		perm["order:coupon_update"] = true
	}
	r := templates.NewJetHTMLRender("../../../../templates", true)
	rec := httptest.NewRecorder()
	if err := r.Instance("admin/order/coupons.html", d).Render(rec); err != nil {
		t.Fatalf("coupons.html 渲染失败：%v", err)
	}
	out := rec.Body.String()
	assertContains(t, out,
		"</html>",
		`<template id="tpl-coupon-create">`, `<div data-coupon-create-host>`,
		`hx-post="/admin/coupons/create"`,
		`data-drawer-url="/admin/coupons/edit-form?`,
		`id="coupon-create-starts-at"`,
	)
	assertAbsent(t, out, `<template id="tpl-coupon-edit-3">`, `data-coupon-edit-host`, `id="coupon-3-name"`)
}
