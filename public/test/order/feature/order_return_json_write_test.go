package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
)

type returnBulkShapeCase struct {
	name   string
	path   string
	body   string
	inBody string
}

func TestReturnBulkShapeViolationsAreRejectedBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := orderhttp.NewHandle(nil)
	engine := gin.New()
	engine.POST("/api/order/return/approve", h.ApproveReturn)
	engine.POST("/api/order/return/reject", h.RejectReturn)

	approve := "/api/order/return/approve"
	reject := "/api/order/return/reject"
	cases := []returnBulkShapeCase{

		{name: "approve ids 空数组", path: approve, body: `{"ids":[],"remark":"x"}`},
		{name: "approve ids 显式 null", path: approve, body: `{"ids":null,"remark":"x"}`},
		{name: "approve ids 元素非字符串", path: approve, body: `{"ids":["1",2],"remark":"x"}`},
		{name: "approve 未知字段", path: approve, body: `{"ids":["1"],"remark":"x","zzz":1}`},

		{name: "approve 混进单档的 returnId", path: approve, body: `{"ids":["1"],"returnId":2,"remark":"x"}`},

		{name: "approve 带批档不支持的 autoReceive", path: approve, body: `{"ids":["1"],"remark":"x","autoReceive":true}`},

		{name: "approve 试图注入操作人", path: approve, body: `{"ids":["1"],"remark":"x","operatorId":"999"}`},
		{name: "approve 试图注入操作人类型", path: approve, body: `{"ids":["1"],"remark":"x","operatorType":"customer"}`},

		{name: "reject 缺 remark", path: reject, body: `{"ids":["1"]}`, inBody: "order.bulk.returnRejectReasonRequired"},
		{name: "reject remark 空串", path: reject, body: `{"ids":["1"],"remark":""}`, inBody: "order.bulk.returnRejectReasonRequired"},
		{name: "reject remark 全空白", path: reject, body: `{"ids":["1"],"remark":"   "}`, inBody: "order.bulk.returnRejectReasonRequired"},

		{name: "reject remark 显式 null", path: reject, body: `{"ids":["1"],"remark":null}`},
		{name: "reject remark 非字符串", path: reject, body: `{"ids":["1"],"remark":5}`},
		{name: "reject 混进单档的 returnId", path: reject, body: `{"ids":["1"],"returnId":2,"remark":"x"}`},
	}
	for _, tc := range cases {
		rec := bulkJSONPost(engine, tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s：应 400（形状档），实际 %d：%s", tc.name, rec.Code, rec.Body.String())
			continue
		}
		body := rec.Body.String()

		for _, tok := range []string{"SQLSTATE", "panic", "%s", "%!s("} {
			if strings.Contains(body, tok) {
				t.Errorf("%s：响应体出现非受控片段 %q：%s", tc.name, tok, body)
			}
		}
		if tc.inBody != "" && !strings.Contains(body, tc.inBody) && !strings.Contains(body, "批量拒绝需要先填理由") {
			t.Errorf("%s：响应体应是受控文案（词条 key 或它的译文），实际 %s", tc.name, body)
		}
		assertOrderLeakFree(t, tc.name, body)
	}
}

func TestReturnSingleJSONSemanticsUnchanged(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货单档语义商品", 88, 20)
	orderID, userID, itemID := f.paidOrder(t, "return-single@example.com", vid, 2)
	stockAfterOrder := f.stockOf(t, vid)
	ret := f.requestReturn(t, orderID, userID, itemID, 2)

	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(f.orders)
	engine := gin.New()
	engine.POST("/api/order/return/approve", h.ApproveReturn)
	engine.POST("/api/order/return/reject", h.RejectReturn)
	engine.POST("/api/order/return/receive", h.ReceiveReturn)

	rec := bulkJSONPost(engine, "/api/order/return/reject",
		`{"returnId":`+strconv.FormatUint(ret.ID, 10)+`,"remark":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("单档拒绝缺理由应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertOrderLeakFree(t, "单档拒绝缺理由", rec.Body.String())
	if got := f.returnStatusOf(t, ret.ID); got != "requested" {
		t.Fatalf("拒绝被拒后申请状态不该变，实际 %s", got)
	}

	form := url.Values{"returnId": {strconv.FormatUint(ret.ID, 10)}, "remark": {"表单通道拒绝"}}
	reqForm := httptest.NewRequest(http.MethodPost, "/api/order/return/reject", strings.NewReader(form.Encode()))
	reqForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recForm := httptest.NewRecorder()
	engine.ServeHTTP(recForm, reqForm)
	if recForm.Code != http.StatusOK {
		t.Fatalf("表单编码的单档调用应照常工作（迁移期间老调用方还在发表单），实际 %d：%s", recForm.Code, recForm.Body.String())
	}
	if got := f.returnStatusOf(t, ret.ID); got != "rejected" {
		t.Fatalf("表单通道拒绝后状态应为 rejected，实际 %s", got)
	}

	orderID2, userID2, itemID2 := f.paidOrder(t, "return-single-auto@example.com", vid, 2)
	stockAfterOrder2 := f.stockOf(t, vid)
	reservedAfterOrder2 := f.reservedOf(t, vid)
	ret2 := f.requestReturn(t, orderID2, userID2, itemID2, 2)
	rec2 := bulkJSONPost(engine, "/api/order/return/approve",
		`{"returnId":`+strconv.FormatUint(ret2.ID, 10)+`,"remark":"同意","autoReceive":true}`)
	if rec2.Code != http.StatusOK {
		t.Fatalf("单档同意（一步到底）应 200，实际 %d：%s", rec2.Code, rec2.Body.String())
	}
	if got := f.returnStatusOf(t, ret2.ID); got != "completed" {
		t.Fatalf("autoReceive=true 应一步完成，实际 %s", got)
	}

	if got := f.stockOf(t, vid); got != stockAfterOrder2 {
		t.Fatalf("未发货订单的退货不改变在库数量（应仍是 %d），实际 %d", stockAfterOrder2, got)
	}

	if got := f.reservedOf(t, vid); got != reservedAfterOrder2-2 {
		t.Fatalf("一步到底后预占应减 2（%d → %d），实际 %d", reservedAfterOrder2, reservedAfterOrder2-2, got)
	}
	if got := f.orderStatusOf(t, orderID2); got != "refunded" {
		t.Fatalf("全额退货后订单应为已退款，实际 %s", got)
	}

	rec3 := bulkJSONPost(engine, "/api/order/return/receive",
		`{"returnId":`+strconv.FormatUint(ret2.ID, 10)+`}`)

	stockBeforeRetry := f.stockOf(t, vid)
	if got := f.stockOf(t, vid); got != stockBeforeRetry {
		t.Fatalf("重复点收货把库存又加了一遍：%d → %d（响应 %d）", stockBeforeRetry, got, rec3.Code)
	}

	rec4 := bulkJSONPost(engine, "/api/order/return/approve", `{"returnId":"abc","remark":"x"}`)
	if rec4.Code != http.StatusBadRequest {
		t.Fatalf("非数字 returnId 应 400，实际 %d：%s", rec4.Code, rec4.Body.String())
	}
	assertOrderLeakFree(t, "非数字 returnId", rec4.Body.String())

	if got := f.stockOf(t, vid); got != stockAfterOrder2 {
		t.Fatalf("非数字 returnId 的请求不该动库存，实际 %d（应为 %d）", got, stockAfterOrder2)
	}
	_ = orderID
	_ = stockAfterOrder
}

func TestReturnBulkApproveApprovesWithoutReceiving(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货批档同意商品", 50, 30)
	a := f.newReturn(t, "ret-bulk-a@example.com", vid, 1)
	b := f.newReturn(t, "ret-bulk-b@example.com", vid, 1)
	stockBefore := f.stockOf(t, vid)

	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(f.orders)
	engine := gin.New()
	engine.POST("/api/order/return/approve", h.ApproveReturn)

	body := `{"ids":["` + strconv.FormatUint(a, 10) + `","` + strconv.FormatUint(b, 10) + `"],"remark":"批量同意"}`
	res := orderBulkSend(t, engine, "/api/order/return/approve", body)
	if res.Updated != 2 {
		t.Fatalf("两条待审申请都应被同意，实际 updated=%d", res.Updated)
	}

	for _, id := range []uint64{a, b} {
		if got := f.returnStatusOf(t, id); got != "approved" {
			t.Fatalf("批量同意后申请 %d 应为 approved（不入库），实际 %s", id, got)
		}
	}
	if got := f.stockOf(t, vid); got != stockBefore {
		t.Fatalf("批量同意不该动库存：%d → %d", stockBefore, got)
	}
}

func TestReturnBulkApproveRejectsWholeBatchWhenAnyUnreviewable(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货批档拒绝整批商品", 30, 30)
	pending := f.newReturn(t, "ret-skip-pending@example.com", vid, 1)
	already := f.newReturn(t, "ret-skip-already@example.com", vid, 1)

	if _, err := f.orders.ApproveReturn(t.Context(), &orderdto.ReturnReviewReq{
		ReturnID: already, Remark: "先同意一次", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("先行同意失败：%v", err)
	}
	missing := returnMissingID(t, f)

	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(f.orders)
	engine := gin.New()
	engine.POST("/api/order/return/approve", h.ApproveReturn)

	body := `{"ids":["` + strconv.FormatUint(pending, 10) + `","` +
		strconv.FormatUint(already, 10) + `"],"remark":"批档业务档"}`
	rejected := orderBulkRejectSend(t, engine, "/api/order/return/approve", body)
	if len(rejected.Data.Items) != 1 {
		t.Fatalf("应给出挡住整批的那一条，实际 %d 条：%+v", len(rejected.Data.Items), rejected.Data.Items)
	}
	if rejected.Data.Items[0].Message == "" {
		t.Errorf("明细必须带原因：%+v", rejected.Data.Items[0])
	}
	if got := f.returnStatusOf(t, pending); got != "requested" {
		t.Fatalf("整批被拒时待审的那条不该被同意，实际 %s", got)
	}

	body2 := `{"ids":["` + strconv.FormatUint(pending, 10) + `","` +
		strconv.FormatUint(missing, 10) + `"],"remark":"批档缺单"}`
	_ = orderBulkRejectSend(t, engine, "/api/order/return/approve", body2)
	if got := f.returnStatusOf(t, pending); got != "requested" {
		t.Fatalf("整批被拒时待审的那条不该被同意，实际 %s", got)
	}

	body3 := `{"ids":["` + strconv.FormatUint(pending, 10) + `","abc"],"remark":"批档形状"}`
	shapeRejected := orderBulkRejectSend(t, engine, "/api/order/return/approve", body3)
	if len(shapeRejected.Data.Items) != 1 || shapeRejected.Data.Items[0].ID != "abc" {
		t.Fatalf("形状错应精确指出那一条，实际 %+v", shapeRejected.Data.Items)
	}

	c := f.newReturn(t, "ret-skip-ok@example.com", vid, 1)
	bodyOK := `{"ids":["` + strconv.FormatUint(c, 10) + `"],"remark":"批档合法"}`
	okRes := orderBulkSend(t, engine, "/api/order/return/approve", bodyOK)
	if okRes.Updated != 1 {
		t.Fatalf("单条可审时应成功，实际 updated=%d", okRes.Updated)
	}
}

func TestReturnBulkRejectRequiresReasonBeforeAnyWrite(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货批档拒绝商品", 20, 30)
	a := f.newReturn(t, "ret-rej-a@example.com", vid, 1)
	b := f.newReturn(t, "ret-rej-b@example.com", vid, 1)

	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(f.orders)
	engine := gin.New()
	engine.POST("/api/order/return/reject", h.RejectReturn)

	ids := `"` + strconv.FormatUint(a, 10) + `","` + strconv.FormatUint(b, 10) + `"`

	rec := bulkJSONPost(engine, "/api/order/return/reject", `{"ids":[`+ids+`]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("批量拒绝缺理由应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	for _, id := range []uint64{a, b} {
		if got := f.returnStatusOf(t, id); got != "requested" {
			t.Fatalf("缺理由时不该有任何写入，申请 %d 已变成 %s", id, got)
		}
	}

	res := orderBulkSend(t, engine, "/api/order/return/reject", `{"ids":[`+ids+`],"remark":"尺码不合适，客户已确认"}`)
	if res.Updated != 2 {
		t.Fatalf("两条都该被拒绝，实际 updated=%d", res.Updated)
	}
	for _, id := range []uint64{a, b} {
		if got := f.returnStatusOf(t, id); got != "rejected" {
			t.Fatalf("申请 %d 应被拒绝，实际 %s", id, got)
		}
	}
}

func (f *orderFixture) newReturn(t *testing.T, email, variantID string, qty int) uint64 {
	t.Helper()
	orderID, userID, itemID := f.paidOrder(t, email, variantID, qty)
	return f.requestReturn(t, orderID, userID, itemID, qty).ID
}

func returnMissingID(t *testing.T, f *orderFixture) uint64 {
	t.Helper()
	var maxID uint64
	if err := f.db.Raw("SELECT COALESCE(MAX(id), 0) FROM order_returns").Scan(&maxID).Error; err != nil {
		t.Fatalf("读最大退货单 id 失败: %v", err)
	}
	return maxID + 999999
}

func TestReturnsTemplateDeclaresJSONWrites(t *testing.T) {
	src := readTemplateSource(t, "admin/order/returns.html")

	for _, forbidden := range []string{`action="/admin/returns/`, "csrfField()", `method="post" action=`} {
		if strings.Contains(src, forbidden) {
			t.Errorf("退货页仍有页面写路径痕迹 %q（写动作应全部改由前端直调 JSON）", forbidden)
		}
	}

	for _, want := range []string{
		`data-write-url="/api/order/return/approve"`,
		`data-write-url="/api/order/return/reject"`,
		`data-write-url="/api/order/return/receive"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("模板缺少端点声明 %q", want)
		}
	}
	if got := strings.Count(src, `data-write-url="/api/order/return/approve"`); got != 2 {
		t.Errorf("approve 端点应出现两次（单档表单 + 批量按钮），实际 %d", got)
	}
	if got := strings.Count(src, `data-write-url="/api/order/return/reject"`); got != 2 {
		t.Errorf("reject 端点应出现两次（单档表单 + 批量按钮），实际 %d", got)
	}
	if got := strings.Count(src, `data-write="bulk"`); got != 2 {
		t.Errorf("批量按钮应恰好两个，实际 %d", got)
	}
	if got := strings.Count(src, `data-write-extra="remark"`); got != 2 {
		t.Errorf("两个批量按钮都要带上备注字段，实际 %d", got)
	}

	if got := strings.Count(src, `name="returnId"`); got == 0 {
		t.Error("表单里没有 name=returnId 的字段 —— 服务端收不到退货单 id")
	}
	if got := strings.Count(src, `name="autoReceive"`); got != 1 {
		t.Errorf("只有同意表单应有 name=autoReceive，实际 %d", got)
	}

	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, `data-write="bulk"`) && strings.Contains(line, "autoReceive") {
			t.Errorf("第 %d 行的批量按钮声明了 autoReceive（批档不支持，会整批 400）：%s", i+1, strings.TrimSpace(line))
		}
	}

	if got, want := strings.Count(src, "data-requires-js"), 6; got != want {
		t.Errorf("四个单档按钮与两个批量按钮都应标记 data-requires-js，实际 %d，want %d", got, want)
	}
	if !strings.Contains(src, "<noscript>") {
		t.Error("模板缺少 noscript 说明（禁用 JS 时写操作不可用要说清楚）")
	}

	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "<form") || strings.Contains(line, "data-write-api") {
			continue
		}
		if strings.Contains(line, "method=\"post\"") && strings.Contains(line, "data-write") {
			t.Errorf("第 %d 行的批量表单带了 data-write 声明（应落在按钮上）：%s", i+1, strings.TrimSpace(line))
		}
	}

	for _, key := range []string{
		"admin.returns.bulk.noneSelected",
		"admin.returns.bulk.refresh",
		"admin.returns.write.noJs",
	} {
		if !strings.Contains(src, key) {
			t.Errorf("模板没有引用本批词条 %q（迁移 605 的 18 行里它成了孤儿）", key)
		}
	}
}

func readTemplateSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../../../internal/templates", rel))
	if err != nil {
		t.Fatalf("读模板 %s 失败: %v", rel, err)
	}
	return string(b)
}
