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

// bulkResult 批档**成功**响应的 data 段（字段名即 Go 字段名，见 order_bulk_write.go）。
//
// 没有 skipped / items：批量写要么全成、要么全败（AGENTS.md §写操作）。
// 失败走 409 + data.items（见 bulkReject）。
type bulkResult struct {
	Updated int `json:"Updated"`
}

// bulkReject 批档**失败**响应的解析结果（409 + data.items）。
type bulkReject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Items []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Message string `json:"message"`
		} `json:"items"`
	} `json:"data"`
}

// orderBulkSend 用真装配发一个批档请求，返回 data 段。
func orderBulkSend(t *testing.T, engine *gin.Engine, path, body string) bulkResult {
	t.Helper()
	rec := bulkJSONPost(engine, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("批档全部可执行时应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data bulkResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("批档响应不是预期 JSON：%v（%s）", err, rec.Body.String())
	}
	return envelope.Data
}

// orderBulkRejectSend 发一个预期**整批被拒**的批档请求，返回 409 明细。
func orderBulkRejectSend(t *testing.T, engine *gin.Engine, path, body string) bulkReject {
	t.Helper()
	rec := bulkJSONPost(engine, path, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("整批被拒应回 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var out bulkReject
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("拒绝响应不是预期 JSON：%v（%s）", err, rec.Body.String())
	}
	if len(out.Data.Items) == 0 {
		t.Fatalf("整批被拒必须给 data.items（用户要知道是哪一条、为什么）")
	}
	return out
}

// TestOrderBulkBusinessRejectionsRejectWholeBatch 业务档：整批 409 + data.items，
// 且**一条都不许改**（AGENTS.md §写操作：批量要么全成、要么全败）。
//
// 旧行为是「成功 N / 跳过 M」逐条回执。用户拿到那个结果既不知道是哪一条挡住的，
// 也没法只重试那一条 —— 而这一批本可以整批回滚、改完再提交。
func TestOrderBulkBusinessRejectionsRejectWholeBatch(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "批档业务档商品", 20, 50)
	mkOrder := func(email string) uint64 {
		req := f.createBaseReq(vid, 1)
		req.CustomerEmail = email
		res, err := f.orders.CreateOrder(context.Background(), req)
		if err != nil {
			t.Fatalf("建单失败: %v", err)
		}
		return res.ID
	}
	ok := mkOrder("bulk-biz-ok@example.com")
	badEdge := mkOrder("bulk-biz-edge@example.com")

	gin.SetMode(gin.TestMode)
	h := orderhttp.NewHandle(f.orders)
	engine := gin.New()
	engine.POST("/api/order/status", h.ChangeStatus)
	engine.POST("/api/order/cancel", h.CancelOrder)

	// ① pending 单直接流到 completed：状态机不允许 ⇒ 整批拒绝。
	body := `{"ids":["` + strconv.FormatUint(ok, 10) + `","` + strconv.FormatUint(badEdge, 10) +
		`"],"toStatus":"completed","remark":"批档业务档"}`
	rejected := orderBulkRejectSend(t, engine, "/api/order/status", body)
	if rejected.Code != http.StatusConflict {
		t.Errorf("拒绝响应的 code 应与 HTTP 状态一致（409），实际 %d", rejected.Code)
	}
	// 明细必须能定位：每条一行，带 id 与原因文案。
	if len(rejected.Data.Items) != 1 {
		t.Fatalf("应给出挡住整批的那一条，实际 %d 条：%+v", len(rejected.Data.Items), rejected.Data.Items)
	}
	if rejected.Data.Items[0].ID == "" || rejected.Data.Items[0].Message == "" {
		t.Errorf("明细缺 id 或原因：%+v", rejected.Data.Items[0])
	}
	// 整批不动：两张单都还停在 pending。
	for _, id := range []uint64{ok, badEdge} {
		if got := f.orderStatusOf(t, id); got != "pending" {
			t.Errorf("订单 %d 在整批被拒后改了状态：%s（批量必须全败）", id, got)
		}
	}

	// ② 已取消的单一混进来 ⇒ 整批拒绝，可取消的那张也不能被取消。
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
	_ = orderBulkRejectSend(t, engine, "/api/order/cancel", string(payload))
	if got := f.orderStatusOf(t, cancelable); got != "pending" {
		t.Errorf("整批被拒时可取消的订单不该被取消，实际 %s", got)
	}

	// ③ 反证：整批都可执行时照常成功（证明拒绝不是"批量永远失败"）。
	fine := mkOrder("bulk-biz-fine@example.com")
	fine2 := mkOrder("bulk-biz-fine2@example.com")
	payload2, _ := json.Marshal(map[string]any{
		"ids":    []string{strconv.FormatUint(fine, 10), strconv.FormatUint(fine2, 10)},
		"remark": "批档合法取消",
	})
	okRes := orderBulkSend(t, engine, "/api/order/cancel", string(payload2))
	if okRes.Updated != 2 {
		t.Errorf("两条都能取消时应更新 2 条，实际 %d", okRes.Updated)
	}
}
