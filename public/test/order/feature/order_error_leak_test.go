package feature

// order_error_leak_test.go — 订单后台页不把 error 原文送进响应（审计 CQ-009 / 第三波收口）。
//
// 本批（受控类型批）把三个页面（订单 / 优惠码 / 退货）里「shell.BulkIDs 的失败被 err.Error()
// 直接送进响应」的那一路，改成调用 shell.BulkIDsFacingText：超限错误是带 sentinel 的类型
// （bulkids.ErrTooMany / *bulkids.IDsError），于是「当前 M 项」（去重后的条数）也回到了
// 页面上 —— 那个数只有 shell 知道，模块自己重算就是第二份真相（旧实现正是丢掉了它）。
//
// 断言因此是结构性的：① 响应里是受控文案且**不含任何内部细节指纹**；② 上限与本次条数
// **两个数字都在**，且来自类型字段而不是从错误原文里掐出来的（出口只认类型、不认文本，
// 见 internal/shell/bulk_test.go 的「包装不泄漏」用例）。
// 只断言「没有 SQLSTATE」不足以区分「真的受控」与「恰好这句话里没有敏感词」。
//
// 传输通道的契约变化（架构改造后，三轮）：
//   1. 写动作的结论不再经 URL（302 + ?err= / ?ok= / ?done=），而是由 shell.RenderJump 渲染
//      **提示页**（HTTP 200，文案在响应体里）。
//   2. 订单列表页的六个写动作迁到「前端直调 JSON」（static/js/ui/write-base.js）：批量走既有
//      端点的**批档形状**（/api/order/status、/api/order/cancel，体里有 ids 即批档，见
//      internal/module/order/inbound/http/order_bulk_write.go）。
//   3. 退货页的两个批量动作收同一档（task-98 S3）：批档形状挂到 /api/order/return/{approve,reject}
//      （见 internal/module/order/inbound/http/return_bulk_write.go），页面 POST 整批退役 ——
//      order 模块**再也没有**提示页出口（shell.RenderJump 消费者归零）。
// 两档的回执因此同为 **400 + 受控 key 的带参协议**（`shell.err.bulkIdsTooMany|上限|条数`，由
// pkg/response 按 `|` 后的参数填成成品句）；安全判据完全相同（受控文案 + 不含内部细节指纹 +
// 上限与本次条数两个数字都在），差别只在端点与形状白名单，所以这里仍分两组断言。
//
// 读侧那套「查询参数不是可信边界」的白名单判定（orderFacingText / orderDoneTexts /
// orderPageDone）随通道一起整批删除 —— 文案不进 URL，就没有「伪造的 ?err= 被当成提示」这条
// 攻击面。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

// orderLeakTokens 内部细节指纹：PG 原文与库结构里一定出现、业务文案里一定不出现。
var orderLeakTokens = []string{"SQLSTATE", "uq_", "pg_", `relation "`, "constraint"}

// assertOrderLeakFree 断言给定文本不含任何内部细节指纹。
func assertOrderLeakFree(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range orderLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

// orderOverLimitJSON 构造超限的**批档**请求体（体里有 ids 即批档，多给一条即触发上限）。
func orderOverLimitJSON(extra map[string]any) string {
	payload := map[string]any{"ids": orderOverLimitIDs()}
	for k, v := range extra {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// orderOverLimitIDs 超限的 id 列表（bulkids.MaxIDs + 1 条）。
func orderOverLimitIDs() []string {
	n := bulkids.MaxIDs + 1
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	return ids
}

// orderPOSTJSON 发一个 JSON POST（写动作迁移后的真实通道）。
func orderPOSTJSON(engine *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestOrderBulkPagesRejectOversizedSelectionWithControlledText 批量入口在 id 超限时回的
// **受控文案**与 err.Error() 无关（原文里的计数器、类型名都不会跟着出去）。
//
// 两条通道各自断言：
//
//	· 退货的两条页面批量入口（S3 未迁）：shell.RenderJump 渲染提示页（HTTP 200），
//	  文案进响应体、不进 URL —— 「伪造的 ?err= 冒充提示」这条攻击面已被整块删掉；
//	· 订单的两条批档入口（已迁 JSON）：400 + 受控 key 的带参协议，由 pkg/response 填成成品句。
//
// 优惠码的两条批量入口同在 JSON 通道上，由 order_coupon_write_test.go 覆盖（那里是券的
// 业务口径），本用例不重复。
//
// 用 nil service 是刻意的最小化：超限在读到 service 之前就被 bulkids.Normalize 拒绝，
// 这条路径本来就不该碰数据库。
func TestOrderBulkPagesRejectOversizedSelectionWithControlledText(t *testing.T) {
	// 词条必须**自己注入**，不能靠包内别的测试的副作用。
	//
	// 这个测试的引擎不建 i18n 组件，而 pkg/response 的 translate 靠 pkg/i18n 取词 ——
	// 没有词条时会回落成 key 原文（`shell.err.bulkIdsTooMany|200|201`），断言就假红。
	// 实测：单跑必失败、整包跑时因为 trade_empty_state_honesty_test.go 先注入过而通过 ——
	// 这是**测试顺序依赖**，换包内文件顺序或单独 -run 就翻脸。
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行",
			"en-US": "Too many items at once: max %s, got %s"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	gin.SetMode(gin.TestMode)
	orderHandle := orderhttp.NewHandle(nil)

	engine := gin.New()
	// 渲染器仍要挂：本文件后半段（TestOrderPageHidesInternalError）走的是真页面 GET。
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 写路径全部是「前端直调 JSON」：超限在批档形状校验里被拒。
	engine.POST("/api/order/status", orderHandle.ChangeStatus)
	engine.POST("/api/order/cancel", orderHandle.CancelOrder)
	engine.POST("/api/order/return/approve", orderHandle.ApproveReturn)
	engine.POST("/api/order/return/reject", orderHandle.RejectReturn)

	// 反证：**形状校验真正经过的汇合点**（pkg/bulkids.Normalize，表单档与 JSON 批档共用同一个）
	// 给出的 error 原文里带「当前 N 项」的计数 —— 它是给日志看的，不是给页面的。
	_, rawErr := bulkids.Normalize(orderOverLimitIDs())
	if rawErr == nil {
		t.Fatalf("反证失败：ids 超过 %d 条时 bulkids.Normalize 应报错", bulkids.MaxIDs)
	}
	over := strconv.Itoa(bulkids.MaxIDs + 1)
	if !strings.Contains(rawErr.Error(), over) {
		t.Fatalf("反证失败：原文应带本次条数 %s，实际 %v", over, rawErr)
	}

	// 共同判据抽成一个断言：受控文案出现 + 两个数字都在 + 不含内部细节。
	assertControlled := func(t *testing.T, where, text string) {
		t.Helper()
		if !strings.Contains(text, "一次最多操作") {
			t.Errorf("%s 应回带「一次最多操作」的受控文案，实际 %q", where, text)
		}
		// 文案由**受控类型**给出，所以「当前 M 项」这个只有 shell 知道的去重后条数也应当出现。
		// 少了 M 说明文案又退回了模块自己拼的版本。
		if !strings.Contains(text, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(text, over) {
			t.Errorf("%s 应带上限 %d 与本次条数 %s（由受控类型给回），实际 %q", where, bulkids.MaxIDs, over, text)
		}
		assertOrderLeakFree(t, where, text)
	}

	// ① 退货的两条批量入口（S3 已迁 JSON 批档）：400 + 受控回执，且**不再有 Location**。
	//
	// 批量拒绝要求先填原因，否则整批不处理；补上这一项才能走到 id 上限那一步
	// （缺理由的 400 是另一条判据，见 order_return_write_test.go）。
	// 超限在批档形状校验里就被拒（normalize 在读到 service 之前），本用例的 nil service
	// 因此一次都不会被碰到 —— 真碰到了会 panic。
	for _, tc := range []struct{ path, body string }{
		{"/api/order/return/approve", orderOverLimitJSON(map[string]any{"remark": "测试"})},
		{"/api/order/return/reject", orderOverLimitJSON(map[string]any{"remark": "测试"})},
	} {
		rec := orderPOSTJSON(engine, tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 超限应回 400，实际 %d：%s", tc.path, rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("%s 不应再用 302 + ?err= 回带文案，实际 Location=%q", tc.path, loc)
		}
		assertControlled(t, tc.path+" 的批档回执", rec.Body.String())
	}

	// ② 订单的两条批档入口：超限即 400（形状先于服务，没碰数据库就判出来了），
	//    回执只能是「受控 key 的带参协议」或它被 pkg/response 填成的成品句 —— 两者按
	//    Accept-Language 都可能出现（本用例不设语言头），所以判据落在**信息量**上：
	//    上限与本次条数两个数字都在、且不含原文指纹/未填参模板（与 S1 的券批档同口径）。
	assertBatchControlled := func(t *testing.T, where, text string) {
		t.Helper()
		if !strings.Contains(text, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(text, over) {
			t.Errorf("%s 应带上限 %d 与本次条数 %s（由受控类型给回），实际 %q", where, bulkids.MaxIDs, over, text)
		}
		// "一次最多操作 %s" 是**未填参**的模板：出现它说明有人把 raw 模板当成品发了，
		// 或者填参链路断了（页面上会看到裸 %s）。
		for _, tok := range []string{"bulkids:", "IDsError", "一次最多操作 %s"} {
			if strings.Contains(text, tok) {
				t.Errorf("%s 泄漏原文/未填参模板 %q：%s", where, tok, text)
			}
		}
		assertOrderLeakFree(t, where, text)
	}
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/api/order/status", orderOverLimitJSON(map[string]any{"toStatus": "paid"})},
		{"/api/order/cancel", orderOverLimitJSON(map[string]any{"remark": "测试"})},
	} {
		rec := orderPOSTJSON(engine, tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 超限应回 400，实际 %d：%s", tc.path, rec.Code, rec.Body.String())
		}
		assertBatchControlled(t, tc.path+" 的批档回执", rec.Body.String())
	}
}

// TestOrderPageHidesInternalError 订单列表取数撞上真实 PG 错误（表被删）：
// 页面降级渲染完整页面（200），只给归口文案，不带表名 / SQLSTATE。
//
// 新契约：归口文案不再走 `c.String(500, …)` 或 `?err=`，而是进模板的 `.LoadErr`
// （订单模板里是一条 `role="alert"` 的 badge，配空态段落「这一页的数据没能读出来」）。
// 判据不变：**原文一个片段都不进响应体**，页面显示的是 adminui.PageInternalText 的中文。
func TestOrderPageHidesInternalError(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/admin/orders", orderhttp.NewOrderPageHandle(f.orders, projects, nil).OrdersPage)

	// 制造**真实**基础设施错误。
	if err := f.db.Exec("DROP TABLE IF EXISTS orders CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}
	// 反证：原文带表名与 SQLSTATE。
	_, rawErr := f.orders.ListOrders(context.Background(), &orderdto.ListOrderReq{
		ProjectID: f.projectID, Limit: 5,
	})
	if rawErr == nil {
		t.Fatal("反证失败：表已删除，ListOrders 却成功了")
	}
	if !strings.Contains(rawErr.Error(), `relation "orders"`) || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误应带表名与 SQLSTATE，实际 %v", rawErr)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/orders?project="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败必须降级渲染完整页面（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("装载失败未渲染完整页面（缺 </html>）—— 页壳被整块吃掉")
	}
	assertOrderLeakFree(t, "订单列表页响应体", body)
	for _, tok := range []string{"relation", "SQLSTATE", "42P01"} {
		if strings.Contains(body, tok) {
			t.Errorf("订单列表页响应体泄漏内部细节 %q", tok)
		}
	}
	if !strings.Contains(body, "系统内部错误，请稍后重试") {
		t.Errorf("页面应显示归口文案（经 .LoadErr badge），实际未出现（渲染中断？）")
	}
}
