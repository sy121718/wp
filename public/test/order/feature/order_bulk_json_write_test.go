package feature

// order_bulk_json_write_test.go — 批档形状的**两档语义**（形状档 400 / 业务档 200+skipped）。
//
// 背景：订单列表页的批量流转与批量取消现在是既有 JSON 端点的**新形状**（体里有 `ids` 即批档，
// 见 internal/module/order/inbound/http/order_bulk_write.go）。两档的界线必须钉死：
//
//   · **形状档**（缺字段 / 空串 / 全空白 / 显式 null / 类型不对 / 多字段 / ids 不合法）
//     → 400 + 受控文案，且**零 service 调用**；
//   · **业务档**（目标状态合法，但订单自己不可流转：状态机不允许这条边、已发货、单不存在）
//     → 200 + 逐条 skipped + 原因，批量不因一条非法边整批停下。
//
// 形状档这一侧用 **nil service** 做硬证据：真调了 service 就是 nil 解引用 panic ——
// 「形状先行、没碰数据库」因此不是一句注释，而是用例能证明的事实（与 S1 券批档同手法）。
// 反过来，业务档必须用真装配：只有真状态机才能说清「pending 不能直接 completed」。
//
// 这条界线是**口径修正**的产物：迁 JSON 之前，空 `toStatus` 会掉进逐条裁决，回执变成
// 「0 个已流转、N 个被跳过」—— 客户端漏字段与业务不可流转混成同一句话，用户看到「一条都没做」，
// 排查方向从第一次就错了（模板漏声明 data-write-extra 时的真实现象）。

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
)

// orderBulkShapeCase 形状档的一个输入形态。
type orderBulkShapeCase struct {
	name   string
	path   string
	body   string
	inBody string // 期望出现在响应体里的辨识片段（受控文案：词条 key 或它的翻译）
}

// bulkJSONPost 发一个 JSON POST（不装 i18n 的裸引擎：形状判定与 service 无关）。
func bulkJSONPost(engine *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestOrderBulkShapeViolationsAreRejectedBeforeService 形状档：一律 400，且**一个 service 调用都没有**。
func TestOrderBulkShapeViolationsAreRejectedBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// nil service：形状判定若真的走到 service，这里会 panic（硬证据，不靠注释与日志）。
	h := orderhttp.NewHandle(nil)
	engine := gin.New()
	engine.POST("/api/order/status", h.ChangeStatus)
	engine.POST("/api/order/cancel", h.CancelOrder)

	cases := []orderBulkShapeCase{
		{name: "缺 toStatus", path: "/api/order/status", body: `{"ids":["1"]}`},
		{name: "toStatus 空串", path: "/api/order/status", body: `{"ids":["1"],"toStatus":""}`},
		{name: "toStatus 全空白", path: "/api/order/status", body: `{"ids":["1"],"toStatus":"   "}`},
		{name: "toStatus 显式 null", path: "/api/order/status", body: `{"ids":["1"],"toStatus":null}`},
		{name: "toStatus 非字符串", path: "/api/order/status", body: `{"ids":["1"],"toStatus":2}`},
		{name: "多一个未知字段", path: "/api/order/status", body: `{"ids":["1"],"toStatus":"paid","zzz":1}`},
		{name: "混进单档的 orderId", path: "/api/order/status", body: `{"ids":["1"],"orderId":2,"toStatus":"paid"}`},
		{name: "ids 空数组", path: "/api/order/status", body: `{"ids":[],"toStatus":"paid"}`},
		{name: "ids 显式 null", path: "/api/order/status", body: `{"ids":null,"toStatus":"paid"}`},
		{name: "ids 元素非字符串", path: "/api/order/status", body: `{"ids":["1",2],"toStatus":"paid"}`},
		{name: "批量取消缺 remark", path: "/api/order/cancel", body: `{"ids":["1"]}`,
			inBody: "order.bulk.cancelReasonRequired"},
		{name: "批量取消 remark 空串", path: "/api/order/cancel", body: `{"ids":["1"],"remark":""}`,
			inBody: "order.bulk.cancelReasonRequired"},
		{name: "批量取消 remark 非字符串", path: "/api/order/cancel", body: `{"ids":["1"],"remark":5}`},
		{name: "批量取消多一个 toStatus", path: "/api/order/cancel", body: `{"ids":["1"],"remark":"x","toStatus":"paid"}`},
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
		if tc.inBody != "" && !strings.Contains(body, tc.inBody) && !strings.Contains(body, "批量取消需要先填原因") {
			t.Errorf("%s：响应体应是受控文案（词条 key 或它的译文），实际 %s", tc.name, body)
		}
	}
}

// bulkResult 批档响应的 data 段（字段名即 Go 字段名，见 order_bulk_write.go 的类型声明）。
type bulkResult struct {
	Updated int `json:"Updated"`
	Skipped int `json:"Skipped"`
	Items   []struct {
		ID      string `json:"ID"`
		Updated bool   `json:"Updated"`
		Message string `json:"Message"`
	} `json:"Items"`
}

// orderBulkSend 用真装配发一个批档请求，返回 data 段。
func orderBulkSend(t *testing.T, engine *gin.Engine, path, body string) bulkResult {
	t.Helper()
	rec := bulkJSONPost(engine, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("批档应 200（业务不可流转是逐条 skipped，不是形状错），实际 %d：%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data bulkResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("批档响应不是预期 JSON：%v（%s）", err, rec.Body.String())
	}
	return envelope.Data
}

// TestOrderBulkBusinessRejectionsSkipWithoutStoppingTheBatch 业务档：200 + 逐条 skipped，
// 一条不可流转不影响其余订单，且被跳过的订单状态**一个字节都没变**。
func TestOrderBulkBusinessRejectionsSkipWithoutStoppingTheBatch(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, variantID := f.addProduct(t, "批档业务档商品", 12, 30)

	mkOrder := func(email string) uint64 {
		req := f.createBaseReq(variantID, 1)
		req.CustomerEmail = email
		res, err := f.orders.CreateOrder(t.Context(), req)
		if err != nil {
			t.Fatalf("造单失败（%s）：%v", email, err)
		}
		return res.ID
	}
	ok := mkOrder("bulk-biz-ok@example.com")
	badEdge := mkOrder("bulk-biz-edge@example.com")
	// 不存在的单：取当前最大 id 之后很远的值（与删除类的 missing-row 用例同手法）。
	var maxID uint64
	if err := f.db.Raw("SELECT COALESCE(MAX(id), 0) FROM orders").Scan(&maxID).Error; err != nil {
		t.Fatalf("读最大订单 id 失败: %v", err)
	}
	missingID := maxID + 999999

	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(f.orders)
	engine := gin.New()
	engine.POST("/api/order/status", h.ChangeStatus)
	engine.POST("/api/order/cancel", h.CancelOrder)

	// ① 一条合法边 + 一条状态机不允许的边 + 一条不存在的单。
	body := `{"ids":["` + strconv.FormatUint(ok, 10) + `","` + strconv.FormatUint(badEdge, 10) +
		`","` + strconv.FormatUint(missingID, 10) + `"],"toStatus":"completed","remark":"批档业务档"}`
	res := orderBulkSend(t, engine, "/api/order/status", body)
	if res.Updated != 0 {
		t.Errorf("pending 单不能直接流到 completed，应全部跳过，实际 updated=%d", res.Updated)
	}
	if res.Skipped != 3 {
		t.Errorf("三条都该跳过，实际 skipped=%d（响应：%+v）", res.Skipped, res)
	}
	if len(res.Items) != 3 {
		t.Fatalf("应逐条回执 3 条，实际 %d", len(res.Items))
	}
	seenIDs := map[string]bool{}
	for _, it := range res.Items {
		seenIDs[it.ID] = true
		if it.Updated {
			t.Errorf("订单 %s 不该被标记成功", it.ID)
		}
		if it.Message == "" {
			t.Errorf("被跳过的订单 %s 必须带上原因（否则页面只看到「被跳过」，不知道为什么）", it.ID)
		}
	}
	for _, id := range []uint64{ok, badEdge, missingID} {
		if !seenIDs[strconv.FormatUint(id, 10)] {
			t.Errorf("逐条回执里缺少订单 %d（回执必须与请求的 ids 一一对应）", id)
		}
	}
	// 被跳过的订单状态一个都不许变。
	for _, id := range []uint64{ok, badEdge} {
		if got := f.orderStatusOf(t, id); got != "pending" {
			t.Errorf("订单 %d 被跳过却改了状态：%s（业务档不接受部分写入）", id, got)
		}
	}

	// ② 同一批里一条能取消、一条已经取消过 ⇒ 后者 skipped，前者照常完成。
	cancelable := mkOrder("bulk-biz-cancel-ok@example.com")
	already := mkOrder("bulk-biz-cancel-twice@example.com")
	if _, err := f.orders.CancelOrder(t.Context(), &orderdto.CancelOrderReq{
		OrderID: already, Reason: "先取消一次", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("先取消一次失败：%v", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"ids":    []string{strconv.FormatUint(cancelable, 10), strconv.FormatUint(already, 10)},
		"remark": "批档业务档取消",
	})
	res2 := orderBulkSend(t, engine, "/api/order/cancel", string(payload))
	if res2.Updated != 1 || res2.Skipped != 1 {
		t.Errorf("应 1 成功 1 跳过，实际 updated=%d skipped=%d（%+v）", res2.Updated, res2.Skipped, res2)
	}
	if got := f.orderStatusOf(t, cancelable); got != "cancelled" {
		t.Errorf("可取消的订单应被取消，实际 %s", got)
	}
	if got := f.orderStatusOf(t, already); got != "cancelled" {
		t.Errorf("已取消的订单状态不该被二次改动，实际 %s", got)
	}
}
