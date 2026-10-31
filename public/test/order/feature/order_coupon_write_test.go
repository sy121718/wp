package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
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

	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	bulkids "go_wp/pkg/bulkids"
)

var (
	couponFormRe  = regexp.MustCompile(`(?s)<form\b[^>]*>.*?</form>`)
	couponInputRe = regexp.MustCompile(`<input\b[^>]*>`)
)

func couponWriteEngine(t *testing.T, f *orderFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(f.orders)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", uint64(1))
		c.Set("username", "qa.coupon")
		c.Next()
	})
	engine.POST("/api/order/coupon/create", h.CreateCoupon)
	engine.POST("/api/order/coupon/update", h.UpdateCoupon)
	engine.POST("/api/order/coupon/delete", h.DeleteCoupon)
	return engine
}

func couponJSONPost(engine *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func couponFormPost(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func couponMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("回执不是 JSON: %s", rec.Body.String())
	}
	msg, _ := resp["message"].(string)
	return msg
}

func couponBulkData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("回执不是 JSON: %s", rec.Body.String())
	}
	data, _ := resp["data"].(map[string]any)
	if data == nil {
		t.Fatalf("批量回执应带 data 段（逐条结果）：%s", rec.Body.String())
	}
	return data
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

func TestOrderCouponDeleteAcceptsJSONIDAndLegacyCouponID(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	engine := couponWriteEngine(t, f)

	byID := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "JSON-ID", Name: "新口径键名", DiscountType: "fixed", DiscountValue: 100,
	})
	byLegacy := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "JSON-LEGACY", Name: "历史键名", DiscountType: "fixed", DiscountValue: 100,
	})

	rec := couponJSONPost(engine, "/api/order/coupon/delete", `{"id":`+strconv.FormatUint(byID.ID, 10)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("单档 {\"id\":N} 应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if couponAlive(t, f, byID.ID) {
		t.Fatal("单档删除后券应已不存在")
	}

	rec = couponJSONPost(engine, "/api/order/coupon/delete", `{"couponId":`+strconv.FormatUint(byLegacy.ID, 10)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("单档 {\"couponId\":N} 应 200（历史键兼容），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if couponAlive(t, f, byLegacy.ID) {
		t.Fatal("历史键删除后券应已不存在")
	}

	both := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "JSON-BOTH", Name: "混形", DiscountType: "fixed", DiscountValue: 100,
	})
	rec = couponJSONPost(engine, "/api/order/coupon/delete",
		`{"id":`+strconv.FormatUint(both.ID, 10)+`,"couponId":`+strconv.FormatUint(both.ID, 10)+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("id 与 couponId 同时给应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !couponAlive(t, f, both.ID) {
		t.Fatal("混形请求不得产生副作用：券应仍在")
	}

	rec = couponJSONPost(engine, "/api/order/coupon/delete", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("既无 id 也无 couponId 应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}

	rec = couponJSONPost(engine, "/api/order/coupon/delete",
		`{"id":"`+strconv.FormatUint(both.ID, 10)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("字符串 id 应 400（类型严格），实际 %d：%s", rec.Code, rec.Body.String())
	}

	rec = couponJSONPost(engine, "/api/order/coupon/delete",
		`{"couponId":"`+strconv.FormatUint(both.ID, 10)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("字符串 couponId 应 400（类型严格），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !couponAlive(t, f, both.ID) {
		t.Fatal("类型错的请求不得产生副作用：券应仍在")
	}
}

func TestOrderCouponDeleteBulkDedupesAndReportsPerItem(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	engine := couponWriteEngine(t, f)

	keep := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "BULK-KEEP", Name: "留着", DiscountType: "fixed", DiscountValue: 100,
	})
	gone := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "BULK-GONE", Name: "删掉", DiscountType: "fixed", DiscountValue: 100,
	})
	before := couponTotal(t, f)

	body := `{"ids":["` + strconv.FormatUint(gone.ID, 10) + `","999999999","` +
		strconv.FormatUint(gone.ID, 10) + `"]}`
	rec := couponJSONPost(engine, "/api/order/coupon/delete", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("批档删除应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	data := couponBulkData(t, rec)
	if got := data["deleted"]; got != float64(1) {
		t.Fatalf("去重后应删 1 条（重复 id 去重、缺的 id 记 skipped），实际 deleted=%v：%s", got, rec.Body.String())
	}
	if got := data["skipped"]; got != float64(1) {
		t.Fatalf("缺的 id 应记 skipped=1，实际 %v：%s", got, rec.Body.String())
	}
	items, _ := data["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("逐条结果应有 2 条（去重后），实际 %d：%s", len(items), rec.Body.String())
	}
	if couponAlive(t, f, gone.ID) {
		t.Fatal("批档删除后目标券应已不存在")
	}
	if !couponAlive(t, f, keep.ID) {
		t.Fatal("未出现在 ids 里的券不得被动")
	}
	if got := couponTotal(t, f); got != before-1 {
		t.Fatalf("总券数应恰好少 1（%d → %d）", before, got)
	}

	for _, tok := range []string{"SQLSTATE", "42P01", "pq:", "constraint", `relation "`} {
		if strings.Contains(rec.Body.String(), tok) {
			t.Errorf("回执泄漏内部细节 %q：%s", tok, rec.Body.String())
		}
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

	rec := couponJSONPost(engine, "/api/order/coupon/update",
		`{"ids":["`+strconv.FormatUint(cp.ID, 10)+`"],"status":0}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("批档启停应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	data := couponBulkData(t, rec)
	if got := data["updated"]; got != float64(1) {
		t.Fatalf("应更新 1 条，实际 %v：%s", got, rec.Body.String())
	}
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

	rec = couponJSONPost(engine, "/api/order/coupon/update",
		`{"ids":["`+strconv.FormatUint(cp.ID, 10)+`"],"status":1,"projectId":"other-project"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("越界档应 200（逐条跳过而不是整批失败），实际 %d：%s", rec.Code, rec.Body.String())
	}
	data = couponBulkData(t, rec)
	if got := data["skipped"]; got != float64(1) {
		t.Fatalf("越界券应记 skipped=1，实际 %v：%s", got, rec.Body.String())
	}
	if after, _ = f.orders.GetCoupon(context.Background(), cp.ID); after == nil || after.Status != 0 {
		t.Fatalf("越界请求不得改状态，实际 %v", after)
	}

	for _, body := range []string{
		`{"ids":["` + strconv.FormatUint(cp.ID, 10) + `"],"status":"1"}`,
		`{"ids":["` + strconv.FormatUint(cp.ID, 10) + `"],"status":2}`,
		`{"ids":["` + strconv.FormatUint(cp.ID, 10) + `"],"status":null}`,
	} {
		rec = couponJSONPost(engine, "/api/order/coupon/update", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("目标状态 %s 应 400，实际 %d：%s", body, rec.Code, rec.Body.String())
		}
	}
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
		body string
	}{
		{"混形：单档键与批档键同时出现", "/api/order/coupon/delete", `{"ids":["` + id + `"],"couponId":` + id + `}`},
		{"混形：id 与 ids 同时出现", "/api/order/coupon/delete", `{"id":` + id + `,"ids":["` + id + `"]}`},
		{"未知键", "/api/order/coupon/delete", `{"ids":["` + id + `"],"force":true}`},
		{"ids 空数组", "/api/order/coupon/delete", `{"ids":[]}`},
		{"ids 显式 null", "/api/order/coupon/delete", `{"ids":null}`},
		{"ids 含 null 项", "/api/order/coupon/delete", `{"ids":["` + id + `",null]}`},
		{"ids 是数字数组", "/api/order/coupon/delete", `{"ids":[` + id + `]}`},
		{"ids 是字符串而不是数组", "/api/order/coupon/delete", `{"ids":"` + id + `"}`},
		{"顶层不是对象", "/api/order/coupon/delete", `["` + id + `"]`},
		{"启停档未知键", "/api/order/coupon/update", `{"ids":["` + id + `"],"status":0,"extra":1}`},
		{"启停档 ids 空数组", "/api/order/coupon/update", `{"ids":[],"status":0}`},
		{"启停档缺 status", "/api/order/coupon/update", `{"ids":["` + id + `"]}`},
	}
	for _, tc := range cases {
		rec := couponJSONPost(engine, tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s：应 400，实际 %d：%s", tc.name, rec.Code, rec.Body.String())
		}
	}
	if got := couponTotal(t, f); got != before {
		t.Fatalf("形状违规不得产生副作用：券数 %d → %d", before, got)
	}
	if !couponAlive(t, f, cp.ID) || !couponAlive(t, f, keep.ID) {
		t.Fatal("形状违规不得删掉任何券")
	}

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 1; i <= bulkids.MaxIDs+1; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	payload, _ := json.Marshal(map[string]any{"ids": ids})
	rec := couponJSONPost(engine, "/api/order/coupon/delete", string(payload))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	msg := couponMessage(t, rec)

	if !strings.Contains(msg, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(msg, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("受控提示应带上限 %d 与本次条数 %d，实际 %q", bulkids.MaxIDs, bulkids.MaxIDs+1, msg)
	}
	for _, tok := range []string{"bulkids:", "IDsError", "一次最多操作 %s"} {
		if strings.Contains(msg, tok) {
			t.Errorf("响应应是受控文案（翻译后或协议原文），不得出现原文/未填参模板 %q：%s", tok, msg)
		}
	}
	if got := couponTotal(t, f); got != before {
		t.Fatalf("超限不得部分执行：券数 %d → %d", before, got)
	}
}

func couponOverLimitBody(t *testing.T) string {
	t.Helper()
	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 1; i <= bulkids.MaxIDs+1; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	payload, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatalf("拼超限 body 失败: %v", err)
	}
	return string(payload)
}

func TestOrderCouponBulkShapeChecksRunBeforeAnyServiceCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(nil)
	engine := gin.New()
	engine.POST("/api/order/coupon/delete", h.DeleteCoupon)
	engine.POST("/api/order/coupon/update", h.UpdateCoupon)

	for _, tc := range []struct{ name, path, body string }{
		{"ids 空数组", "/api/order/coupon/delete", `{"ids":[]}`},
		{"ids 显式 null", "/api/order/coupon/delete", `{"ids":null}`},
		{"ids 含 null 项", "/api/order/coupon/delete", `{"ids":["1",null]}`},
		{"ids 是数字数组", "/api/order/coupon/delete", `{"ids":[1]}`},
		{"混形 id+ids", "/api/order/coupon/delete", `{"id":1,"ids":["1"]}`},
		{"混形 couponId+ids", "/api/order/coupon/delete", `{"couponId":1,"ids":["1"]}`},
		{"未知键", "/api/order/coupon/delete", `{"ids":["1"],"force":true}`},
		{"单档无键", "/api/order/coupon/delete", `{}`},
		{"单档 id 为 0", "/api/order/coupon/delete", `{"id":0}`},
		{"id 与 couponId 同时给", "/api/order/coupon/delete", `{"id":1,"couponId":1}`},
		{"超限", "/api/order/coupon/delete", couponOverLimitBody(t)},
		{"启停非法状态 2", "/api/order/coupon/update", `{"ids":["1"],"status":2}`},
		{"启停状态为字符串", "/api/order/coupon/update", `{"ids":["1"],"status":"1"}`},
		{"启停缺状态", "/api/order/coupon/update", `{"ids":["1"]}`},
		{"启停 ids 空数组", "/api/order/coupon/update", `{"ids":[],"status":0}`},
	} {
		rec := couponJSONPost(engine, tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s：应 400（且不得触达 service，service 为 nil），实际 %d：%s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

func TestOrderCouponTemplatesDeclareNumericFieldsForJSONWrite(t *testing.T) {
	const dir = "../../../../internal/templates/admin/order/"
	cases := []struct {
		file string

		formIDPrefix string
		api          string
		mode         string
		number       string
	}{
		{"coupons.html", "coupon-toggle-", "/api/order/coupon/update", "create",
			"id,discountValue,minSubtotal,maxUses,perUserLimit,status"},
		{"coupons.html", "coupon-del-", "/api/order/coupon/delete", "single", "id"},
		{"coupon_create_form.html", "", "/api/order/coupon/create", "create",
			"discountValue,minSubtotal,maxUses,perUserLimit,status"},
		{"coupon_edit_form.html", "", "/api/order/coupon/update", "create",
			"id,discountValue,minSubtotal,maxUses,perUserLimit,status"},
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
			`data-write-url="` + tc.api + `"`,
			`data-write="` + tc.mode + `"`,
			`data-msg-failed=`,
		} {
			if !strings.Contains(block, want) {
				t.Errorf("%s：表单缺少 %s", tc.file, want)
			}
		}
		for _, problem := range couponFormNumericProblems(tc.file, block, tc.number) {
			t.Error(problem)
		}
	}

	bulk, err := os.ReadFile(dir + "coupons.html")
	if err != nil {
		t.Fatalf("读 coupons.html 失败: %v", err)
	}
	for _, want := range []string{
		`data-write-url="/api/order/coupon/update" data-write="bulk" data-write-outcome="update"`,
		`data-write-extra="status,projectId"`,
		`data-write-url="/api/order/coupon/delete" data-write="bulk" data-write-outcome="delete"`,
		`data-msg-too-many="`,
		`data-requires-js hidden`,
	} {
		if !strings.Contains(string(bulk), want) {
			t.Errorf("coupons.html 的批量条缺少 %s", want)
		}
	}

	if m := couponBulkFormTag(string(bulk)); m != "" {
		if strings.Contains(m, "data-write-api") {
			t.Errorf("批量表单不得声明 data-write-api（应落在按钮上）：%s", m)
		}
	}
}

func couponFormNumericProblems(file, block, number string) []string {
	var problems []string
	declared := map[string]bool{}
	for _, name := range strings.Split(number, ",") {
		declared[strings.TrimSpace(name)] = true
	}

	for name := range declared {
		if name == "" {
			problems = append(problems, file+`：data-write-number 里有空字段名`)
			continue
		}
		if !strings.Contains(block, `name="`+name+`"`) {
			problems = append(problems, file+`：data-write-number 声明了不存在的字段 `+name)
		}
	}

	for _, tag := range couponInputTags(block) {
		if !strings.Contains(tag, `type="number"`) {
			continue
		}
		name := couponAttrOf(tag, "name")
		if name == "" {
			problems = append(problems, file+`：有 type=number 控件没有 name：`+tag)
			continue
		}
		if !declared[name] {
			problems = append(problems, file+`：数值控件 `+name+` 未在 data-write-number 里声明（会被当字符串发出）`)
		}
	}
	return problems
}

func TestOrderCouponNumericFieldCheckerCatchesMutations(t *testing.T) {
	raw, err := os.ReadFile("../../../../internal/templates/admin/order/coupon_edit_form.html")
	if err != nil {
		t.Fatalf("读模板失败: %v", err)
	}
	const good = "id,discountValue,minSubtotal,maxUses,perUserLimit,status"
	block, ok := couponFormBlock(string(raw), "")
	if !ok {
		t.Fatal("找不到编辑抽屉表单")
	}
	if problems := couponFormNumericProblems("edit", block, good); len(problems) != 0 {
		t.Fatalf("真实模板应通过判据，实际报错 %v", problems)
	}

	if problems := couponFormNumericProblems("edit", block, strings.Replace(good, ",maxUses", "", 1)); len(problems) == 0 {
		t.Error("少声明 maxUses 应被判据抓住（否则「漏声明」这条通道没有守卫）")
	}

	mutated := strings.Replace(block, `<input type="number" class="form-input" id="coupon-{{id}}-max-uses"`,
		`<input type="number" class="form-input" name="unlistedNumber" id="coupon-{{id}}-max-uses"`, 1)
	if mutated == block {
		t.Fatal("变异②没有改到模板文本（锚点漂移，用例失去意义）")
	}
	if problems := couponFormNumericProblems("edit", mutated, good); len(problems) == 0 {
		t.Error("未声明的 type=number 控件应被判据抓住")
	}

	if problems := couponFormNumericProblems("edit", block, good+",phantomField"); len(problems) == 0 {
		t.Error("声明的幻影字段应被判据抓住")
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

func couponFormBlockExact(body, formID string) (string, bool) {
	for _, m := range couponFormRe.FindAllString(body, -1) {
		if strings.Contains(m, `id="`+formID+`"`) {
			return m, true
		}
	}
	return "", false
}

func couponBulkFormTag(body string) string {
	for _, m := range couponFormRe.FindAllString(body, -1) {
		if strings.Contains(m, `name="ids"`) && strings.Contains(m, `data-write-status`) {
			if end := strings.Index(m, ">"); end > 0 {
				return m[:end+1]
			}
		}
	}
	return ""
}

func couponInputTags(block string) []string {
	return couponInputRe.FindAllString(block, -1)
}

func couponAttrOf(tag, attr string) string {
	marker := attr + `="`
	i := strings.Index(tag, marker)
	if i < 0 {
		return ""
	}
	rest := tag[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func TestOrderCouponSingleUpdateKeepsLegacyBindingAndTimeFormats(t *testing.T) {
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
	if rec.Code != http.StatusOK {
		t.Fatalf("表单编码的单档更新应 200（绑定兼容），实际 %d：%s", rec.Code, rec.Body.String())
	}
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

	rec = couponJSONPost(engine, "/api/order/coupon/update",
		`{"id":`+strconv.FormatUint(cp.ID, 10)+`,"name":"控件档","discountType":"fixed","discountValue":300,`+
			`"minSubtotal":0,"maxUses":0,"perUserLimit":0,"status":1,"startsAt":"2026-02-01T09:30","endsAt":"2026-02-28T23:59"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("datetime-local 形态应被归口后接受，实际 %d：%s", rec.Code, rec.Body.String())
	}
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

	rec = couponJSONPost(engine, "/api/order/coupon/update",
		`{"id":`+strconv.FormatUint(cp.ID, 10)+`,"name":"含时区","discountType":"fixed","discountValue":300,`+
			`"status":1,"startsAt":"2026-02-01T09:30:00Z"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("带时区的写法应仍被拒（不得因归口被接受），实际 %d：%s", rec.Code, rec.Body.String())
	}
}
