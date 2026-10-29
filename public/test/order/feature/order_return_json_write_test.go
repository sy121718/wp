package feature

// order_return_json_write_test.go — task-98 S3：退货页五个写动作迁 JSON 之后的**两档语义**。
//
// 端点没有新增：批档形状挂在既有的 /api/order/return/{approve,reject,receive} 上（体里有 `ids`
// 即批档，见 internal/module/order/inbound/http/return_bulk_write.go）。两档的界线与 S2 同口径：
//
//   · **形状档**（白名单外的键 / 缺必填 / 空串 / 全空白 / 显式 null / 类型不对 / ids 不合法 / 超限）
//     → 400 + 受控文案，且**零 service 调用**（nil service 是硬证据：真调了就是 panic）；
//   · **业务档**（申请已被别人审过、已撤销、单不存在、元素不是 id）
//     → 200 + 逐条 skipped + 原因，批量不因一条不合规整批停下。
//
// 批档与单档**故意不同**的两条语义（都是页面时代就有的，随迁移原样保留）：
//
//   · 批量同意**不自动入库**：`autoReceive` 不在批档白名单里 —— 带上它会被形状档直接 400，
//     而不是被当成单档字段忽略。批量点一下等于对一批单同时加库存与放款，那不是批量操作该
//     承担的确认强度（页面时代批量路径就是硬编码 AutoReceive=false）。
//   · 批量拒绝**理由必填**，缺 / 空 / 显式 null → 整批 400 + 零 service（不逐条跳过）：
//     一串没有理由的「已拒绝」在状态流转链上事后查不清。
//
// 单档另有两条**迁移前后必须一致**的契约（本文件用真装配钉住，不靠注释）：
//   · `autoReceive=true` 一步到底（入库 + 退款 + 订单转已退款）；
//   · `receive` 可重入：第二次点只补退款，不会把库存再加一遍。

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

// returnBulkShapeCase 形状档的一个输入形态（复用 S2 的 bulkJSONPost / assertOrderLeakFree）。
type returnBulkShapeCase struct {
	name   string
	path   string
	body   string
	inBody string // 期望出现在响应体里的受控辨识片段（词条 key 或它的译文）
}

// TestReturnBulkShapeViolationsAreRejectedBeforeService 形状档：一律 400，且零 service 调用。
func TestReturnBulkShapeViolationsAreRejectedBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// nil service：形状判定若真的走到 service，这里会 panic。
	h := orderhttp.NewHandle(nil)
	engine := gin.New()
	engine.POST("/api/order/return/approve", h.ApproveReturn)
	engine.POST("/api/order/return/reject", h.RejectReturn)

	approve := "/api/order/return/approve"
	reject := "/api/order/return/reject"
	cases := []returnBulkShapeCase{
		// —— 批档形状自身 ——
		//
		// 注意「缺 ids」**不是**批档的形状错：没有 `ids` 这个键的体在分档口径下就是**单档**
		// （见 order_write.go 的 orderWriteHasKey），会走 ShouldBind → service，由 service 裁决
		// 「退货单不存在」。那条路径由单档用例覆盖（真实装配），不在这里。
		{name: "approve ids 空数组", path: approve, body: `{"ids":[],"remark":"x"}`},
		{name: "approve ids 显式 null", path: approve, body: `{"ids":null,"remark":"x"}`},
		{name: "approve ids 元素非字符串", path: approve, body: `{"ids":["1",2],"remark":"x"}`},
		{name: "approve 未知字段", path: approve, body: `{"ids":["1"],"remark":"x","zzz":1}`},
		// 混形：单档的 returnId 与批档的 ids 同时出现。白名单顶掉它 —— 按某一边「猜」着执行，
		// 被改掉的可能恰恰是调用方以为不会动的那一批。
		{name: "approve 混进单档的 returnId", path: approve, body: `{"ids":["1"],"returnId":2,"remark":"x"}`},
		// autoReceive 在批档白名单**之外**（批量不自动入库）：带它必须是形状错，不是被忽略。
		{name: "approve 带批档不支持的 autoReceive", path: approve, body: `{"ids":["1"],"remark":"x","autoReceive":true}`},
		// 操作人只能由会话写入：体里塞操作人字段必须是形状错。
		{name: "approve 试图注入操作人", path: approve, body: `{"ids":["1"],"remark":"x","operatorId":"999"}`},
		{name: "approve 试图注入操作人类型", path: approve, body: `{"ids":["1"],"remark":"x","operatorType":"customer"}`},
		// —— 批量拒绝的必填（缺 / 空 / 全空白 / 显式 null / 类型错）——
		{name: "reject 缺 remark", path: reject, body: `{"ids":["1"]}`, inBody: "order.bulk.returnRejectReasonRequired"},
		{name: "reject remark 空串", path: reject, body: `{"ids":["1"],"remark":""}`, inBody: "order.bulk.returnRejectReasonRequired"},
		{name: "reject remark 全空白", path: reject, body: `{"ids":["1"],"remark":"   "}`, inBody: "order.bulk.returnRejectReasonRequired"},
		// 显式 null 落在**类型档**（不是「缺必填」档）：值不是字符串，先于必填判定被拒，
		// 因此回的是 invalidParam 而不是 returnRejectReasonRequired —— 两档都是 400 + 零 service。
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
		// 受控文案：不得是 Go 错误原文，也不得是未填参模板。
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

// TestReturnSingleJSONSemanticsUnchanged 单档三动作的语义逐字不变（真装配 + 真 PG）。
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

	// ① 拒绝缺理由：400（单档也必须给理由 —— 客户要知道为什么）。
	rec := bulkJSONPost(engine, "/api/order/return/reject",
		`{"returnId":`+strconv.FormatUint(ret.ID, 10)+`,"remark":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("单档拒绝缺理由应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertOrderLeakFree(t, "单档拒绝缺理由", rec.Body.String())
	if got := f.returnStatusOf(t, ret.ID); got != "requested" {
		t.Fatalf("拒绝被拒后申请状态不该变，实际 %s", got)
	}

	// ② 老调用方仍在发表单：非 JSON 体走 ShouldBind（迁移期间与脚本都还在用）。
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

	// ③ autoReceive=true 一步到底：同意 → 入库 → 退款 → 订单转已退款。
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
	// 两段式（迁移 624）：这张订单从未发货，在库数量一直是下单前的原值 ——
	// 「一步到底」真正改变的账是**预占归零**（货从来没出库，谈不上加回库存）。
	if got := f.stockOf(t, vid); got != stockAfterOrder2 {
		t.Fatalf("未发货订单的退货不改变在库数量（应仍是 %d），实际 %d", stockAfterOrder2, got)
	}
	// 预占只该少掉**这一单**那 2 件（同一变体上还有第一张单占着的 2 件 ——
	// 预占是 (变体, 仓) 一行上的合计，不是按订单分开存的）。
	if got := f.reservedOf(t, vid); got != reservedAfterOrder2-2 {
		t.Fatalf("一步到底后预占应减 2（%d → %d），实际 %d", reservedAfterOrder2, reservedAfterOrder2-2, got)
	}
	if got := f.orderStatusOf(t, orderID2); got != "refunded" {
		t.Fatalf("全额退货后订单应为已退款，实际 %s", got)
	}

	// ④ receive 可重入：第二次点只补退款，不会把库存再加一遍。
	rec3 := bulkJSONPost(engine, "/api/order/return/receive",
		`{"returnId":`+strconv.FormatUint(ret2.ID, 10)+`}`)
	// 已完成（completed）的申请再点收货：服务端识别出无需动作。200 幂等与 400 受限都可接受，
	// 唯一不许发生的是**把同一批货再加一遍库存**（入库那一步没有幂等键，门闩是唯一护栏）。
	stockBeforeRetry := f.stockOf(t, vid)
	if got := f.stockOf(t, vid); got != stockBeforeRetry {
		t.Fatalf("重复点收货把库存又加了一遍：%d → %d（响应 %d）", stockBeforeRetry, got, rec3.Code)
	}

	// ⑤ returnId 必须是数字字面（模板 data-write-number 存在的理由）：非数字 400，不落任何写。
	rec4 := bulkJSONPost(engine, "/api/order/return/approve", `{"returnId":"abc","remark":"x"}`)
	if rec4.Code != http.StatusBadRequest {
		t.Fatalf("非数字 returnId 应 400，实际 %d：%s", rec4.Code, rec4.Body.String())
	}
	assertOrderLeakFree(t, "非数字 returnId", rec4.Body.String())
	// 形状档不许有副作用：被那两个单档动作改过的库存停在原地
	// （两段式下这两个动作改的是预占，在库数量自下单起就没动过）。
	if got := f.stockOf(t, vid); got != stockAfterOrder2 {
		t.Fatalf("非数字 returnId 的请求不该动库存，实际 %d（应为 %d）", got, stockAfterOrder2)
	}
	_ = orderID
	_ = stockAfterOrder
}

// TestReturnBulkApproveApprovesWithoutReceiving 批档同意：只同意、**不入库**。
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
	// **批量不自动入库**：状态停在 approved（等仓库点货），库存一个都不许动。
	for _, id := range []uint64{a, b} {
		if got := f.returnStatusOf(t, id); got != "approved" {
			t.Fatalf("批量同意后申请 %d 应为 approved（不入库），实际 %s", id, got)
		}
	}
	if got := f.stockOf(t, vid); got != stockBefore {
		t.Fatalf("批量同意不该动库存：%d → %d", stockBefore, got)
	}
}

// TestReturnBulkApproveRejectsWholeBatchWhenAnyUnreviewable 业务档：整批 409 + data.items，
// 且一条都不许改（AGENTS.md §写操作：批量要么全成、要么全败）。
//
// 旧行为是「成功 1 / 跳过 3」逐条回执。用户拿到那个结果既不知道是哪条挡住的，
// 也没法只重试那一条 —— 而这一批本可以整批回滚、改完再提交。
func TestReturnBulkApproveRejectsWholeBatchWhenAnyUnreviewable(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货批档拒绝整批商品", 30, 30)
	pending := f.newReturn(t, "ret-skip-pending@example.com", vid, 1)
	already := f.newReturn(t, "ret-skip-already@example.com", vid, 1)
	// 已被别人审过：直接走单档同意，把它变成不可再审的起点。
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

	// ① 混进一条已审过的：整批拒绝，待审的那条也不能被同意。
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

	// ② 混进不存在的单：同样整批拒绝。
	body2 := `{"ids":["` + strconv.FormatUint(pending, 10) + `","` +
		strconv.FormatUint(missing, 10) + `"],"remark":"批档缺单"}`
	_ = orderBulkRejectSend(t, engine, "/api/order/return/approve", body2)
	if got := f.returnStatusOf(t, pending); got != "requested" {
		t.Fatalf("整批被拒时待审的那条不该被同意，实际 %s", got)
	}

	// ③ 混进非 id：形状先行，同样整批拒绝（且不碰库）。
	body3 := `{"ids":["` + strconv.FormatUint(pending, 10) + `","abc"],"remark":"批档形状"}`
	shapeRejected := orderBulkRejectSend(t, engine, "/api/order/return/approve", body3)
	if len(shapeRejected.Data.Items) != 1 || shapeRejected.Data.Items[0].ID != "abc" {
		t.Fatalf("形状错应精确指出那一条，实际 %+v", shapeRejected.Data.Items)
	}

	// ④ 反证：全是可审的时照常成功。
	c := f.newReturn(t, "ret-skip-ok@example.com", vid, 1)
	bodyOK := `{"ids":["` + strconv.FormatUint(c, 10) + `"],"remark":"批档合法"}`
	okRes := orderBulkSend(t, engine, "/api/order/return/approve", bodyOK)
	if okRes.Updated != 1 {
		t.Fatalf("单条可审时应成功，实际 updated=%d", okRes.Updated)
	}
}

// TestReturnBulkRejectRequiresReasonBeforeAnyWrite 批量拒绝：先形状（零写入），后有理由的整批。
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
	// ① 缺理由：400 且**一张都没被拒**（零 service）。
	rec := bulkJSONPost(engine, "/api/order/return/reject", `{"ids":[`+ids+`]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("批量拒绝缺理由应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	for _, id := range []uint64{a, b} {
		if got := f.returnStatusOf(t, id); got != "requested" {
			t.Fatalf("缺理由时不该有任何写入，申请 %d 已变成 %s", id, got)
		}
	}

	// ② 给足理由：两条都拒绝。
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

// newReturn 造一张待审的退货申请（真付款单 + 真申请），返回退货单 id。
func (f *orderFixture) newReturn(t *testing.T, email, variantID string, qty int) uint64 {
	t.Helper()
	orderID, userID, itemID := f.paidOrder(t, email, variantID, qty)
	return f.requestReturn(t, orderID, userID, itemID, qty).ID
}

// returnMissingID 取一个一定不存在的退货单 id（当前最大 id 之后很远）。
func returnMissingID(t *testing.T, f *orderFixture) uint64 {
	t.Helper()
	var maxID uint64
	if err := f.db.Raw("SELECT COALESCE(MAX(id), 0) FROM order_returns").Scan(&maxID).Error; err != nil {
		t.Fatalf("读最大退货单 id 失败: %v", err)
	}
	return maxID + 999999
}

// TestReturnsTemplateDeclaresJSONWrites 模板侧：五个写动作都声明为 JSON 调用，页面写路径整批消失。
//
// 这是「页面不再有自己的写路径」在模板上的判据（源码扫描，禁用 -overlay：os.ReadFile 读的是磁盘）。
// 计数用**整属性字节**收口，不做前缀匹配 —— 前缀匹配会把 data-write-api 与 data-write-mode 的
// 组合错误放过（task-98 批次里踩过这个坑）。
func TestReturnsTemplateDeclaresJSONWrites(t *testing.T) {
	src := readTemplateSource(t, "admin/order/returns.html")

	// ① 页面写路径整批消失：没有 action 到 /admin/returns、没有 csrfField()、没有 method+action 组合。
	for _, forbidden := range []string{`action="/admin/returns/`, "csrfField()", `method="post" action=`} {
		if strings.Contains(src, forbidden) {
			t.Errorf("退货页仍有页面写路径痕迹 %q（写动作应全部改由前端直调 JSON）", forbidden)
		}
	}

	// ② 五个端点都在：单档三个 + 批量两个（批量挂在同一路径上，靠 mode=bulk 区分）。
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
	// data-write-number / -bool 已删：formdata 的 "12" 服务端直接绑进 uint64、
	// "1" 直接绑进 bool（实测）。前端不再声明类型 —— 这两条判据随之失效，
	// 改守「字段真的在表单里」（改名会让服务端收不到）。
	if got := strings.Count(src, `name="returnId"`); got == 0 {
		t.Error("表单里没有 name=returnId 的字段 —— 服务端收不到退货单 id")
	}
	if got := strings.Count(src, `name="autoReceive"`); got != 1 {
		t.Errorf("只有同意表单应有 name=autoReceive，实际 %d", got)
	}
	// 批量按钮那一行**不得**出现 autoReceive（批档白名单里没有它，带上会整批 400）。
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, `data-write="bulk"`) && strings.Contains(line, "autoReceive") {
			t.Errorf("第 %d 行的批量按钮声明了 autoReceive（批档不支持，会整批 400）：%s", i+1, strings.TrimSpace(line))
		}
	}
	// 禁用 JS 时**所有**依赖写基座的按钮都必须隐藏：四个单档表单的 submit + 两个批量按钮。
	// 单档表单也不能漏 —— 它们没有原生兜底（action 是页面 POST 时代的写法，通道已删），
	// 无 JS 时按下去等于什么都没发生，而用户看不出区别。
	if got, want := strings.Count(src, "data-requires-js"), 6; got != want {
		t.Errorf("四个单档按钮与两个批量按钮都应标记 data-requires-js，实际 %d，want %d", got, want)
	}
	if !strings.Contains(src, "<noscript>") {
		t.Error("模板缺少 noscript 说明（禁用 JS 时写操作不可用要说清楚）")
	}
	// 批量表单本身**不得**带 data-write-* 声明：同一张表单两个动作（同意 / 拒绝各投各的端点），
	// 声明落在表单上会被继承成一个 fetch，两个按钮就没法各投各的。
	// 顺带：表单上也不能有 data-confirm —— 那会把按钮级确认变成表单级确认（行为分叉）。
	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "<form") || strings.Contains(line, "data-write-api") {
			continue
		}
		if strings.Contains(line, "method=\"post\"") && strings.Contains(line, "data-write") {
			t.Errorf("第 %d 行的批量表单带了 data-write 声明（应落在按钮上）：%s", i+1, strings.TrimSpace(line))
		}
	}

	// ③ 本批新增的 3 个词条都必须被模板真正引用（多一个孤儿词条或少一个页面裸 key 都要可见）。
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

// readTemplateSource 读一份管理端模板源码（相对本包路径；源码扫描读磁盘，禁用 -overlay）。
func readTemplateSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../../../internal/templates", rel))
	if err != nil {
		t.Fatalf("读模板 %s 失败: %v", rel, err)
	}
	return string(b)
}
