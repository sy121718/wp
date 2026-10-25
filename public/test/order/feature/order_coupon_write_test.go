package feature

// order_coupon_write_test.go — 优惠码写路径 JSON 化（task-98 S1）的**契约**用例。
//
// 装配口径：直挂真实 handler（orderhttp.NewHandle(f.orders)），**不声称**鉴权 / CSRF 链 ——
// 那两层是路由组中间件的职责（order_router.go 的 authorizedAPI：SessionAuth + CSRF + Casbin），
// 本用例只钉「形状 + 业务语义 + 零副作用」。权限锚点本身没变：页面写路由退役后，
// /api/order/coupon/{create,update,delete} 仍是唯一的授权面（页面路径原先只是借它当 obj）。
// 数据是真的：support.NewMigratedPGTestDB（生产迁移 + 生产种子）+ 真实 service。
//
// 覆盖（每件都带负样本方向，避免「只测顺路那条」）：
//   ① 单档删除认 JSON：`{"id":N}` 与历史键 `{"couponId":N}` 各自独立可用，两个同时给 = 形状错；
//      （迁 JSON 之前这个 handler 读的是 c.PostForm("couponId")，对 JSON 调用方必然 400。）
//   ② 批档严格并集：只认字符串数组 ids，重复去重不报错，逐条给结果，缺的记 skipped；
//      形状违规（null / 类型 / 混形 / 未知键 / 超限）一律**零副作用**（先校验后写库）。
//   ③ 行内启停（单档整体更新）必须回送全部字段 → 只改 status，其余字段不回退成零值；
//      跨工程 + 非法目标状态各自被拒。
//   ④ 时间窗归口**只**为 datetime-local 控件形态服务：`T` 形态被归口，服务端原本就支持的
//      三种写法原样透传（不被改写），带时区的 RFC3339 **仍然拒绝**（不扩大 API 接受的格式面）。
//
// 「页面 POST 已下线」这条不由本文件断言：那是路由层事实，由 routes 快照
//（internal/routers/testdata/routes.snapshot 673 行）与快照用例收口 —— 在这里再挂一次页面路由
// 等于把已删的 handler 复活成测试替身。

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

// 模板静态核验用的解析器（只认模板里实际出现的写法：属性值一律双引号、form/input 都是字面量）。
var (
	couponFormRe  = regexp.MustCompile(`(?s)<form\b[^>]*>.*?</form>`)
	couponInputRe = regexp.MustCompile(`<input\b[^>]*>`)
)

// couponWriteEngine 挂三条真实 JSON 端点，并注入操作人上下文（生产由 SessionAuth 注入，
// 操作人是审计字段：客户端传什么都不看）。
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

// couponJSONPost 发 JSON 体（Content-Type: application/json）。
func couponJSONPost(engine *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// couponFormPost 发表单体（迁 JSON 之前页面用的编码）：单档绑定兼容的独立证据。
func couponFormPost(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// couponMessage 取回执里的 message（受控文案 / key|args 协议都在这里）。
func couponMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("回执不是 JSON: %s", rec.Body.String())
	}
	msg, _ := resp["message"].(string)
	return msg
}

// couponBulkData 取批量回执的 data 段（deleted / updated / skipped / items）。
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

// couponAlive 券是否还在（读不到或已被删都算不在）。
func couponAlive(t *testing.T, f *orderFixture, id uint64) bool {
	t.Helper()
	cp, err := f.orders.GetCoupon(context.Background(), id)
	return err == nil && cp != nil
}

// couponTotal 工程下的券总数：零副作用断言的计数基。
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

// TestOrderCouponDeleteAcceptsJSONIDAndLegacyCouponID 单档删除的两种键名各自独立可用、互斥。
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

	// ① 新口径 {"id":N}：形状修正的核心 —— 迁 JSON 之前这里必然是 400（handler 只读表单 couponId）。
	rec := couponJSONPost(engine, "/api/order/coupon/delete", `{"id":`+strconv.FormatUint(byID.ID, 10)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("单档 {\"id\":N} 应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if couponAlive(t, f, byID.ID) {
		t.Fatal("单档删除后券应已不存在")
	}

	// ② 历史键 {"couponId":N}：为既有调用方保留（不静默破坏），独立验证。
	rec = couponJSONPost(engine, "/api/order/coupon/delete", `{"couponId":`+strconv.FormatUint(byLegacy.ID, 10)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("单档 {\"couponId\":N} 应 200（历史键兼容），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if couponAlive(t, f, byLegacy.ID) {
		t.Fatal("历史键删除后券应已不存在")
	}

	// ③ 两个键同时给 = 形状错：不替调用方选一个，且**零副作用**。
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

	// ④ 两个都不给 = 形状错。
	rec = couponJSONPost(engine, "/api/order/coupon/delete", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("既无 id 也无 couponId 应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 类型严格：JSON 字符串 "5" 不是 id（json.Number 底层是 string，宽松解析会静默接受）。
	rec = couponJSONPost(engine, "/api/order/coupon/delete",
		`{"id":"`+strconv.FormatUint(both.ID, 10)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("字符串 id 应 400（类型严格），实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 历史键同样严格：数字就是数字，字符串 "5" 不是 id（两条键名的类型判据必须同源）。
	rec = couponJSONPost(engine, "/api/order/coupon/delete",
		`{"couponId":"`+strconv.FormatUint(both.ID, 10)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("字符串 couponId 应 400（类型严格），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !couponAlive(t, f, both.ID) {
		t.Fatal("类型错的请求不得产生副作用：券应仍在")
	}
}

// TestOrderCouponDeleteBulkDedupesAndReportsPerItem 批档：严格字符串数组、重复去重、逐条结果。
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

	// 重复 id 由 pkg/bulkids.Normalize 去重（**不**因重复报 400），缺的 id 记 skipped。
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

	// 逐条原因必须是受控文案：不含 SQLSTATE / 表名 / 约束名。
	for _, tok := range []string{"SQLSTATE", "42P01", "pq:", "constraint", `relation "`} {
		if strings.Contains(rec.Body.String(), tok) {
			t.Errorf("回执泄漏内部细节 %q：%s", tok, rec.Body.String())
		}
	}
}

// TestOrderCouponToggleBulkKeepsOtherFieldsAndGuardsProject 批档启停：整体更新语义 + 越界守卫。
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

	// 启停**之前**先真读一次库：时间窗的判据是「前后两次读到的 UTC 时刻逐位相等」——
	// 这才是「批量启停不得改动时间窗」的直接证据（比对照建券入参更强：读→写→读整条往返都算进去了）。
	before, err := f.orders.GetCoupon(context.Background(), cp.ID)
	if err != nil || before == nil || before.StartsAt == nil || before.EndsAt == nil {
		t.Fatalf("建券后读不到时间窗: %v / %+v", err, before)
	}
	preStart := before.StartsAt.Time().UTC()
	preEnd := before.EndsAt.Time().UTC()

	// ① 合法目标状态：只改 status，其余字段一个都不许回退成零值。
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
	// 时间窗读改写不漂移：前后都是 UTC 时刻，逐位相等（打印两侧值，便于失败时直接看出偏移量）。
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

	// ② 跨工程守卫：projectId 给了别的工程 → 该券被跳过（不跨工程改券），状态不动。
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

	// ③ 非法目标状态（字符串 "1" / 2）：形状错 → 400，且状态不动。
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

// TestOrderCouponBulkShapeViolationsHaveNoSideEffects 形状违规一律先校验、后写库。
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

	// 超限：整批 400 + 受控 key 的带参协议（key|max|count），且**不部分执行**。
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
	// 口径：handler 发的是受控 key 的**带参协议** `shell.err.bulkIdsTooMany|上限|本次条数`，
	// 由 pkg/response 按 | 后的参数填模板。本 fixture 装了 i18n（newOrderFixture 走真迁移 + 真组件），
	// 所以响应里是**翻译后的成品句**；nav 那条同口径用例没装 i18n，于是看到的是协议原文 ——
	// 两种都算通过，判定落在「不是原文、且两个数字都在」上（只断言"超限了"会让条数丢了也绿）。
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

// couponOverLimitBody 超限 body（bulkids.MaxIDs + 1 条字符串 id）。
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

// TestOrderCouponBulkShapeChecksRunBeforeAnyServiceCall 形状校验必须**先于**任何 service 调用。
//
// 判据用的是最容易自证的手段：把 service 传 nil —— 只要流程走到 service 一步就是 nil 解引用
// panic（测试直接红），所以「400」本身就证明了「没碰 service」。
// 覆盖 [] / null / 混形 / 类型 / 未知键 / 超限 / 非法目标状态：全都在读库之前就该被拦住。
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

// TestOrderCouponTemplatesDeclareNumericFieldsForJSONWrite 模板层静态交叉核验：
// 每个写表单声明的 `data-write-number` 必须**恰好**等于它实际提交的数值字段。
//
// 为什么非核不可：write-base.js 的 create 模式把**表单具名字段**原样发出去，
// 只有 data-write-number 点名的字段会被转成 JSON 数字。少声明一个 → 该字段以字符串发出 →
// 服务端绑定拒绝（表现是「这个表单永远提交不了」，而字段看着完全正常）；多声明一个 →
// 声明了不存在的字段，后来人照着它找字段会找不到真相。
//
// 这里读模板**文本**而不是渲染后的 DOM：判据只涉及「同一个 form 内的属性 vs 该 form 内的 input」，
// 与渲染数据无关（模板里这些名字都是字面量）；页面级渲染另有 admin_control_selection_test.go。
func TestOrderCouponTemplatesDeclareNumericFieldsForJSONWrite(t *testing.T) {
	const dir = "../../../../internal/templates/admin/order/"
	cases := []struct {
		file string
		// form 定位：coupons.html 里有两段行内表单（行内启停 / 单条删除），用 id 前缀区分；
		// 抽屉片段每个文件只有一段表单，留空即可。
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
		// data-write-number 已删：formdata 的 "3" 服务端直接绑进 int（实测），
		// 前端不再声明类型。这里只守端点、模式与结论文案。
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

	// 批量条的两条按钮：端点 + 模式 + 启停额外字段（按钮优先于表单，属性落在按钮上）。
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
	// 反向：批量表单**不得**自己声明 data-write-api（会被按钮继承成一个 fetch，
	// 目标状态 select 与 ids 的额外字段语义就没了）。
	if m := couponBulkFormTag(string(bulk)); m != "" {
		if strings.Contains(m, "data-write-api") {
			t.Errorf("批量表单不得声明 data-write-api（应落在按钮上）：%s", m)
		}
	}
}

// couponFormNumericProblems 核一个表单段：声明的数值字段与表单实际字段必须一致。
//
// 判据与实现分开是有意的 —— 判据要能被**变异**验证：TestOrderCouponNumericFieldCheckerCatchesMutations
// 直接把改坏的模板文本喂进来，证明这三种错都会被抓住（真实文件不可能为了验证判据去改坏，
// 而 overlay 只影响编译期读到的文件，管不到测试运行时的 os.ReadFile）。
func couponFormNumericProblems(file, block, number string) []string {
	var problems []string
	declared := map[string]bool{}
	for _, name := range strings.Split(number, ",") {
		declared[strings.TrimSpace(name)] = true
	}
	// ① 声明的字段必须真的在这个表单里（不留幻影）。
	for name := range declared {
		if name == "" {
			problems = append(problems, file+`：data-write-number 里有空字段名`)
			continue
		}
		if !strings.Contains(block, `name="`+name+`"`) {
			problems = append(problems, file+`：data-write-number 声明了不存在的字段 `+name)
		}
	}
	// ② 表单里每个 type="number" 控件都必须被声明（否则以字符串发出 → 服务端绑定拒）。
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

// TestOrderCouponNumericFieldCheckerCatchesMutations 判据自身不许空转：正向通过 + 三种变异必被抓。
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
	// 变异①：少声明一个真实存在的数值字段（maxUses）。
	if problems := couponFormNumericProblems("edit", block, strings.Replace(good, ",maxUses", "", 1)); len(problems) == 0 {
		t.Error("少声明 maxUses 应被判据抓住（否则「漏声明」这条通道没有守卫）")
	}
	// 变异②：新增一个未被声明的 type=number 控件。
	mutated := strings.Replace(block, `<input type="number" class="form-input" id="coupon-{{id}}-max-uses"`,
		`<input type="number" class="form-input" name="unlistedNumber" id="coupon-{{id}}-max-uses"`, 1)
	if mutated == block {
		t.Fatal("变异②没有改到模板文本（锚点漂移，用例失去意义）")
	}
	if problems := couponFormNumericProblems("edit", mutated, good); len(problems) == 0 {
		t.Error("未声明的 type=number 控件应被判据抓住")
	}
	// 变异③：声明一个不存在的字段（幻影）。
	if problems := couponFormNumericProblems("edit", block, good+",phantomField"); len(problems) == 0 {
		t.Error("声明的幻影字段应被判据抓住")
	}
}

// couponFormBlock 取第一个匹配的 <form …>…</form> 段：按 id 前缀过滤（空前缀取第一段）。
func couponFormBlock(body, idPrefix string) (string, bool) {
	for _, m := range couponFormRe.FindAllString(body, -1) {
		if idPrefix == "" || strings.Contains(m, `id="`+idPrefix) {
			return m, true
		}
	}
	return "", false
}

// couponFormBlockExact 按**精确表单 id** 取整段表单：链式证据要针对具体那张券
// （列表里可能有多行，`couponFormBlock` 的前缀匹配会拿到第一行 —— 那可能是另一张券，
// 于是「翻转后的目标状态」与「被断言的那张券」就不是同一张，实测踩过）。
func couponFormBlockExact(body, formID string) (string, bool) {
	for _, m := range couponFormRe.FindAllString(body, -1) {
		if strings.Contains(m, `id="`+formID+`"`) {
			return m, true
		}
	}
	return "", false
}

// couponBulkFormTag 批量条所在表单的**开标签**（coupons.html 里它包着勾选框与整张表）。
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

// couponInputTags 段内所有 <input …> 标签（属性顺序无关：调用方自行按属性名判断）。
func couponInputTags(block string) []string {
	return couponInputRe.FindAllString(block, -1)
}

// couponAttrOf 取标签里某个属性的值（属性值一律用双引号，模板里没有单引号写法）。
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

// TestOrderCouponSingleUpdateKeepsLegacyBindingAndTimeFormats 单档绑定兼容 + 时间窗归口的边界。
//
// 归口的**边界**是本用例的重点：只为 datetime-local 的控件形态（带 T）做转换，
// 服务端原本支持的三种写法原样透传（不被改写），带时区的 RFC3339 仍然拒绝 ——
// 归口不是「扩大 API 接受的时间格式面」，也不是「改时区口径」。
func TestOrderCouponSingleUpdateKeepsLegacyBindingAndTimeFormats(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	engine := couponWriteEngine(t, f)

	// ① 表单编码的单档更新仍然可用（迁 JSON 之前页面走的就是这条绑定路径）：不因批档收敛而收紧。
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
		"startsAt":      {"2026-01-01 09:00"}, // 已合法：必须原样透传
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

	// ② datetime-local 控件形态（带 T）被归口成服务端认的写法：不归口就会「动过时间窗就报不合法」。
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

	// ③ 服务端原本支持的三种写法原样可用（逐个独立验证，避免「只测了归口那条」）。
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

	// ④ 带时区的 RFC3339 **仍然拒绝**：归口不扩大 API 接受的时间格式面。
	rec = couponJSONPost(engine, "/api/order/coupon/update",
		`{"id":`+strconv.FormatUint(cp.ID, 10)+`,"name":"含时区","discountType":"fixed","discountValue":300,`+
			`"status":1,"startsAt":"2026-02-01T09:30:00Z"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("带时区的写法应仍被拒（不得因归口被接受），实际 %d：%s", rec.Code, rec.Body.String())
	}
}
