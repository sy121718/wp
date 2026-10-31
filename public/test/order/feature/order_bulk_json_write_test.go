package feature

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

type orderBulkShapeCase struct {
	name   string
	path   string
	body   string
	inBody string
}

func bulkJSONPost(engine *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestOrderBulkShapeViolationsAreRejectedBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)

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

type bulkResult struct {
	Updated int `json:"Updated"`
}

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

	body := `{"ids":["` + strconv.FormatUint(ok, 10) + `","` + strconv.FormatUint(badEdge, 10) +
		`"],"toStatus":"completed","remark":"批档业务档"}`
	rejected := orderBulkRejectSend(t, engine, "/api/order/status", body)
	if rejected.Code != http.StatusConflict {
		t.Errorf("拒绝响应的 code 应与 HTTP 状态一致（409），实际 %d", rejected.Code)
	}

	if len(rejected.Data.Items) != 1 {
		t.Fatalf("应给出挡住整批的那一条，实际 %d 条：%+v", len(rejected.Data.Items), rejected.Data.Items)
	}
	if rejected.Data.Items[0].ID == "" || rejected.Data.Items[0].Message == "" {
		t.Errorf("明细缺 id 或原因：%+v", rejected.Data.Items[0])
	}

	for _, id := range []uint64{ok, badEdge} {
		if got := f.orderStatusOf(t, id); got != "pending" {
			t.Errorf("订单 %d 在整批被拒后改了状态：%s（批量必须全败）", id, got)
		}
	}

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
