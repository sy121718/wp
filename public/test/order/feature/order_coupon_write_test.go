package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

var (
	couponFormRe  = regexp.MustCompile(`(?s)<form\b[^>]*>.*?</form>`)
	couponInputRe = regexp.MustCompile(`<input\b[^>]*>`)
)

// couponWriteEngine 复刻生产接线：失败出口、请求取词与 HTML 渲染都必须在场。
func couponWriteEngine(t *testing.T, f *orderFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(f.orders)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	// 纯成功走共享成功出口（生产 assembly 同款接线）。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", uint64(1))
		c.Set("username", "qa.coupon")
		c.Next()
	})
	engine.POST("/api/order/coupon/create", pageview.HTMLFragment, h.CreateCoupon)
	engine.POST("/api/order/coupon/update", pageview.HTMLFragment, h.UpdateCoupon)
	engine.POST("/api/order/coupon/delete", pageview.HTMLFragment, h.DeleteCoupon)
	return engine
}

// couponFormPost 按标准表单编码提交（普通界面唯一的输入协议）。
func couponFormPost(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func couponAlive(t *testing.T, f *orderFixture, id uint64) bool {
	t.Helper()
	cp, err := f.orders.GetCoupon(context.Background(), id)
	return err == nil && cp != nil
}

func couponTotal(t *testing.T, f *orderFixture) int64 {
	t.Helper()
	res, err := f.orders.ListCoupons(context.Background(), &orderdto.CouponListReq{
		ProjectID: f.projectID, Limit: 200,
	})
	if err != nil {
		t.Fatalf("读券列表失败: %v", err)
	}
	return res.Total
}

func assertCouponHTMLOK(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200 + HTML 片段，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("成功应回 Jet 成功片段：%s", body)
	}
	return body
}

func assertCouponHTMLFail(t *testing.T, rec *httptest.ResponseRecorder, status int) string {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("应 %d，实际 %d：%s", status, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Fatalf("失败应渲染 Jet 错误片段：%s", body)
	}
	for _, tok := range []string{"SQLSTATE", "42P01", "pq:", `relation "`, "order.err.", "order.msg."} {
		if strings.Contains(body, tok) {
			t.Errorf("失败片段泄漏内部标识 %q：%s", tok, body)
		}
	}
	return body
}

func TestOrderCouponDeleteSingleAndBulk(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	engine := couponWriteEngine(t, f)

	single := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "DEL-ONE", Name: "单张", DiscountType: "fixed", DiscountValue: 100,
	})
	rec := couponFormPost(engine, "/api/order/coupon/delete", url.Values{
		"id": {strconv.FormatUint(single.ID, 10)},
	})
	assertCouponHTMLOK(t, rec)
	if couponAlive(t, f, single.ID) {
		t.Fatal("单张删除后券应已不存在")
	}

	// 缺 id / id=0 / id 非法：400，且不得产生副作用。
	keep := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "DEL-KEEP", Name: "留着", DiscountType: "fixed", DiscountValue: 100,
	})
	for _, form := range []url.Values{{}, {"id": {"0"}}, {"id": {"abc"}}} {
		rec = couponFormPost(engine, "/api/order/coupon/delete", form)
		assertCouponHTMLFail(t, rec, http.StatusBadRequest)
	}
	if !couponAlive(t, f, keep.ID) {
		t.Fatal("形状错的请求不得产生副作用：券应仍在")
	}

	// 批量删除：合法重复 id 去重，整批一次提交。
	gone := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "DEL-BULK", Name: "批量删", DiscountType: "fixed", DiscountValue: 100,
	})
	before := couponTotal(t, f)

	bulk := url.Values{}
	bulk.Add("ids", strconv.FormatUint(gone.ID, 10))
	bulk.Add("ids", strconv.FormatUint(gone.ID, 10))
	// 数字别名："0"+id 与 id 是同一张券，按数值身份去重后仍只删一次。
	bulk.Add("ids", "0"+strconv.FormatUint(gone.ID, 10))
	rec = couponFormPost(engine, "/api/order/coupon/delete", bulk)
	body := assertCouponHTMLOK(t, rec)
	// 纯成功只给公共一句提示：删除条数属明细，不进入成功消息。
	if !strings.Contains(body, "操作成功") {
		t.Errorf("批量删除成功应走公共成功出口：%s", body)
	}
	if couponAlive(t, f, gone.ID) {
		t.Fatal("批量删除后目标券应已不存在")
	}
	if !couponAlive(t, f, keep.ID) {
		t.Fatal("未出现在 ids 里的券不得被动")
	}
	if got := couponTotal(t, f); got != before-1 {
		t.Fatalf("总券数应恰好少 1（%d → %d）", before, got)
	}

	// 混合存在 + 不存在：整批 404，已存在的券不得删除。
	mixed := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "DEL-MIXED", Name: "混合", DiscountType: "fixed", DiscountValue: 100,
	})
	before = couponTotal(t, f)
	bulk = url.Values{}
	bulk.Add("ids", strconv.FormatUint(mixed.ID, 10))
	bulk.Add("ids", "999999999")
	rec = couponFormPost(engine, "/api/order/coupon/delete", bulk)
	assertCouponHTMLFail(t, rec, http.StatusNotFound)
	if !couponAlive(t, f, mixed.ID) {
		t.Fatal("整批拒绝后已存在的券不得被删")
	}
	if got := couponTotal(t, f); got != before {
		t.Fatalf("整批拒绝不得产生副作用：券数 %d → %d", before, got)
	}

	// 显式工程越界：整批 404，券不删。
	otherProject := uuid.NewString()
	before = couponTotal(t, f)
	rec = couponFormPost(engine, "/api/order/coupon/delete", url.Values{
		"ids":       {strconv.FormatUint(mixed.ID, 10)},
		"projectId": {otherProject},
	})
	assertCouponHTMLFail(t, rec, http.StatusNotFound)
	if !couponAlive(t, f, mixed.ID) {
		t.Fatal("越界请求不得删券")
	}
	if got := couponTotal(t, f); got != before {
		t.Fatalf("越界请求不得产生副作用：券数 %d → %d", before, got)
	}
}

func TestOrderCouponToggleBulkKeepsOtherFieldsAndGuardsProject(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	engine := couponWriteEngine(t, f)

	cp := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "TOGGLE-1", Name: "国庆活动券", DiscountType: "fixed", DiscountValue: 2000,
		MinSubtotal: 9900, MaxUses: 100, PerUserLimit: 2, Remark: "国庆活动",
		StartsAt: "2026-01-01 09:00", EndsAt: "2026-01-31 23:59",
	})

	before, err := f.orders.GetCoupon(context.Background(), cp.ID)
	if err != nil || before == nil || before.StartsAt == nil || before.EndsAt == nil {
		t.Fatalf("建券后读不到时间窗: %v / %+v", err, before)
	}
	preStart := before.StartsAt.Time().UTC()
	preEnd := before.EndsAt.Time().UTC()

	rec := couponFormPost(engine, "/api/order/coupon/update", url.Values{
		"ids": {strconv.FormatUint(cp.ID, 10)}, "status": {"0"},
	})
	assertCouponHTMLOK(t, rec)
	after, err := f.orders.GetCoupon(context.Background(), cp.ID)
	if err != nil || after == nil {
		t.Fatalf("重读券失败: %v", err)
	}
	if after.Status != 0 {
		t.Fatalf("status 应改成 0，实际 %d", after.Status)
	}
	if after.Name != "国庆活动券" || after.DiscountType != "fixed" || after.DiscountValue != 2000 {
		t.Fatalf("整体更新漏字段：name=%q type=%q value=%d", after.Name, after.DiscountType, after.DiscountValue)
	}
	if after.MinSubtotal != 9900 || after.MaxUses != 100 || after.PerUserLimit != 2 || after.Remark != "国庆活动" {
		t.Fatalf("整体更新漏字段：min=%d maxUses=%d perUser=%d remark=%q",
			after.MinSubtotal, after.MaxUses, after.PerUserLimit, after.Remark)
	}
	if after.StartsAt == nil || after.EndsAt == nil {
		t.Fatalf("启停后时间窗丢失：%+v", after)
	}
	postStart := after.StartsAt.Time().UTC()
	postEnd := after.EndsAt.Time().UTC()
	if !postStart.Equal(preStart) || !postEnd.Equal(preEnd) {
		t.Fatalf("启停改了时间窗：开始 %s → %s，结束 %s → %s",
			preStart.Format(time.RFC3339), postStart.Format(time.RFC3339),
			preEnd.Format(time.RFC3339), postEnd.Format(time.RFC3339))
	}

	// 跨工程：显式给了不匹配的工程时整批 404，不改状态。
	rec = couponFormPost(engine, "/api/order/coupon/update", url.Values{
		"ids": {strconv.FormatUint(cp.ID, 10)}, "status": {"1"}, "projectId": {uuid.NewString()},
	})
	assertCouponHTMLFail(t, rec, http.StatusNotFound)
	if after, _ = f.orders.GetCoupon(context.Background(), cp.ID); after == nil || after.Status != 0 {
		t.Fatalf("越界请求不得改状态，实际 %v", after)
	}

	// status 只接受 0 / 1；缺失或空串都按参数错误拒绝。
	for _, status := range []string{"2", "abc", ""} {
		rec = couponFormPost(engine, "/api/order/coupon/update", url.Values{
			"ids": {strconv.FormatUint(cp.ID, 10)}, "status": {status},
		})
		assertCouponHTMLFail(t, rec, http.StatusBadRequest)
	}
	rec = couponFormPost(engine, "/api/order/coupon/update", url.Values{
		"ids": {strconv.FormatUint(cp.ID, 10)},
	})
	assertCouponHTMLFail(t, rec, http.StatusBadRequest)
	if after, _ = f.orders.GetCoupon(context.Background(), cp.ID); after == nil || after.Status != 0 {
		t.Fatalf("形状错的请求不得改状态，实际 %v", after)
	}
}

func TestOrderCouponBulkShapeViolationsHaveNoSideEffects(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	engine := couponWriteEngine(t, f)

	cp := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "SHAPE-1", Name: "形状", DiscountType: "fixed", DiscountValue: 100,
	})
	keep := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "SHAPE-2", Name: "陪跑", DiscountType: "fixed", DiscountValue: 100,
	})
	before := couponTotal(t, f)
	id := strconv.FormatUint(cp.ID, 10)

	cases := []struct {
		name string
		path string
		form url.Values
	}{
		{"删除 ids 为空", "/api/order/coupon/delete", url.Values{}},
		{"删除 id 为 0", "/api/order/coupon/delete", url.Values{"id": {"0"}}},
		{"启停 ids 为空", "/api/order/coupon/update", url.Values{"ids": {""}, "status": {"0"}}},
		{"启停缺 status", "/api/order/coupon/update", url.Values{"ids": {id}}},
		{"启停 status 非法", "/api/order/coupon/update", url.Values{"ids": {id}, "status": {"2"}}},
		{"删除混合空串", "/api/order/coupon/delete", url.Values{"ids": {id, ""}}},
		{"删除混合非法", "/api/order/coupon/delete", url.Values{"ids": {id, "abc"}}},
		{"启停混合非法", "/api/order/coupon/update", url.Values{"ids": {id, "abc"}, "status": {"0"}}},
	}
	for _, tc := range cases {
		rec := couponFormPost(engine, tc.path, tc.form)
		assertCouponHTMLFail(t, rec, http.StatusBadRequest)
	}
	if got := couponTotal(t, f); got != before {
		t.Fatalf("形状违规不得产生副作用：券数 %d → %d", before, got)
	}
	if !couponAlive(t, f, cp.ID) || !couponAlive(t, f, keep.ID) {
		t.Fatal("形状违规不得删掉任何券")
	}

	// 超限：400，且一片都不删。
	bulk := url.Values{}
	for i := 1; i <= bulkids.MaxIDs+1; i++ {
		bulk.Add("ids", strconv.Itoa(i))
	}
	rec := couponFormPost(engine, "/api/order/coupon/delete", bulk)
	body := assertCouponHTMLFail(t, rec, http.StatusBadRequest)
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("受控提示应带上限 %d 与本次条数 %d：%s", bulkids.MaxIDs, bulkids.MaxIDs+1, body)
	}
	if got := couponTotal(t, f); got != before {
		t.Fatalf("超限不得部分执行：券数 %d → %d", before, got)
	}
}

func TestOrderCouponTemplatesDeclareHTMXWriteForms(t *testing.T) {
	const dir = "../../../../internal/templates/admin/order/"
	cases := []struct {
		file string

		formIDPrefix string
		path         string
	}{
		{"coupons.html", "coupon-toggle-", "/api/order/coupon/update"},
		{"coupons.html", "coupon-del-", "/api/order/coupon/delete"},
		{"coupon_create_form.html", "", "/api/order/coupon/create"},
		{"coupon_edit_form.html", "", "/api/order/coupon/update"},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(dir + tc.file)
		if err != nil {
			t.Fatalf("读模板失败: %v", err)
		}
		block, ok := couponFormBlock(string(raw), tc.formIDPrefix)
		if !ok {
			t.Errorf("%s：找不到目标表单（id 前缀 %q）", tc.file, tc.formIDPrefix)
			continue
		}
		for _, want := range []string{
			`data-write`,
			`hx-post="` + tc.path + `"`,
			`hx-target="find [data-write-result]"`,
			`hx-swap="innerHTML"`,
			`hx-sync="this:drop"`,
		} {
			if !strings.Contains(block, want) {
				t.Errorf("%s：表单缺少 %s", tc.file, want)
			}
		}
		// 退役属性不得回流。
		for _, retired := range []string{"data-write-url", "data-write-outcome", "data-write-extra", "data-msg-failed", "data-confirm"} {
			if strings.Contains(block, retired) {
				t.Errorf("%s：表单仍带已退役属性 %s", tc.file, retired)
			}
		}
	}

	bulk, err := os.ReadFile(dir + "coupons.html")
	if err != nil {
		t.Fatalf("读 coupons.html 失败: %v", err)
	}
	for _, want := range []string{
		`hx-post="/api/order/coupon/update"`,
		`hx-post="/api/order/coupon/delete"`,
		`hx-include="#cp-list-form`,
		`hx-confirm=`,
		`data-requires-js hidden`,
	} {
		if !strings.Contains(string(bulk), want) {
			t.Errorf("coupons.html 的批量条缺少 %s", want)
		}
	}
}

func couponFormBlock(body, idPrefix string) (string, bool) {
	for _, m := range couponFormRe.FindAllString(body, -1) {
		if idPrefix == "" || strings.Contains(m, `id="`+idPrefix) {
			return m, true
		}
	}
	return "", false
}

func TestOrderCouponSingleUpdateNormalizesTimeFormats(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	engine := couponWriteEngine(t, f)

	cp := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "FORM-1", Name: "表单档", DiscountType: "fixed", DiscountValue: 300,
	})
	form := url.Values{
		"id":            {strconv.FormatUint(cp.ID, 10)},
		"name":          {"表单档改名"},
		"discountType":  {"fixed"},
		"discountValue": {"500"},
		"minSubtotal":   {"1000"},
		"maxUses":       {"0"},
		"perUserLimit":  {"0"},
		"status":        {"1"},
		"startsAt":      {"2026-01-01 09:00"},
		"endsAt":        {""},
		"remark":        {"表单改名"},
	}
	rec := couponFormPost(engine, "/api/order/coupon/update", form)
	assertCouponHTMLOK(t, rec)
	after, err := f.orders.GetCoupon(context.Background(), cp.ID)
	if err != nil || after == nil {
		t.Fatalf("重读券失败: %v", err)
	}
	if after.Name != "表单档改名" || after.DiscountValue != 500 || after.MinSubtotal != 1000 {
		t.Fatalf("表单档没有写进去：%+v", after)
	}
	if after.StartsAt == nil || !after.StartsAt.Time().UTC().Equal(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("已合法的写法被改写：StartsAt=%v", after.StartsAt)
	}
	if after.EndsAt != nil {
		t.Fatalf("空串应仍表示「不限」：EndsAt=%v", after.EndsAt)
	}

	// datetime-local 形态由 controller 归口后接受。
	form.Set("startsAt", "2026-02-01T09:30")
	form.Set("endsAt", "2026-02-28T23:59")
	rec = couponFormPost(engine, "/api/order/coupon/update", form)
	assertCouponHTMLOK(t, rec)
	after, err = f.orders.GetCoupon(context.Background(), cp.ID)
	if err != nil || after == nil {
		t.Fatalf("重读券失败: %v", err)
	}
	if after.StartsAt == nil || !after.StartsAt.Time().UTC().Equal(time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("控件形态归口后时刻应不变：StartsAt=%v", after.StartsAt)
	}
	if after.EndsAt == nil || !after.EndsAt.Time().UTC().Equal(time.Date(2026, 2, 28, 23, 59, 0, 0, time.UTC)) {
		t.Fatalf("控件形态归口后时刻应不变：EndsAt=%v", after.EndsAt)
	}

	for _, tc := range []struct {
		code string
		raw  string
		want time.Time
	}{
		{"FMT-SEC", "2026-03-01 09:00:05", time.Date(2026, 3, 1, 9, 0, 5, 0, time.UTC)},
		{"FMT-MIN", "2026-03-01 09:00", time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)},
		{"FMT-DAY", "2026-03-01", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
	} {
		created := f.addCoupon(t, &orderdto.CouponSaveReq{
			Code: tc.code, Name: "时间格式", DiscountType: "fixed", DiscountValue: 100,
			StartsAt: tc.raw,
		})
		got, gerr := f.orders.GetCoupon(context.Background(), created.ID)
		if gerr != nil || got == nil || got.StartsAt == nil {
			t.Fatalf("%s：建券后读不到时间窗（%v）", tc.code, gerr)
		}
		if !got.StartsAt.Time().UTC().Equal(tc.want) {
			t.Errorf("%s：%q 应解析成 %s，实际 %s", tc.code, tc.raw, tc.want.Format(time.RFC3339), got.StartsAt.Time().UTC().Format(time.RFC3339))
		}
	}

	// 带时区的写法仍被拒（不得因归口被放宽）。
	form.Set("startsAt", "2026-02-01T09:30:00Z")
	rec = couponFormPost(engine, "/api/order/coupon/update", form)
	assertCouponHTMLFail(t, rec, http.StatusBadRequest)
}
