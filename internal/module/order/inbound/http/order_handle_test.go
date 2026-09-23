package orderhttp

// order_handle_test.go — POST /api/order/create 的来源收口回归。
//
// 判据：HTTP JSON 中的 createdVia 不属于建单请求形状；入口调用专属服务方法。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeCreateOrderService 只接住 CreateOrder 的入参。
// 嵌入接口只为满足 Handle 的依赖形状；本用例只触达 CreateOrder，其余方法不会执行。
type fakeCreateOrderService struct {
	ordercontract.OrderService
	got      *orderdto.CreateOrderReq
	apiCalls int
}

func (f *fakeCreateOrderService) CreateOrder(_ context.Context,
	_ *orderdto.CreateOrderReq) (*orderdto.CreateOrderResp, error) {
	panic("站点 API 不应调用 checkout 建单入口")
}

func (f *fakeCreateOrderService) CreateAPIOrder(_ context.Context,
	req *orderdto.CreateOrderReq) (*orderdto.CreateOrderResp, error) {
	f.got = req
	f.apiCalls++
	return &orderdto.CreateOrderResp{ID: 1, OrderNo: "GWP20260101DEADBEEF"}, nil
}

// postCreateOrder 以给定 JSON 体与登录态调用建单 handler，返回响应 recorder。
func postCreateOrder(t *testing.T, h *Handle, body string, withOperator bool) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/order/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if withOperator {
		// 模拟后台登录态：operatorFromContext 从这里取操作人（服务端审计字段）。
		c.Set("user_id", int64(7))
	}
	h.CreateOrder(c)
	return w
}

// TestCreateOrderReqExcludesCreatedVia 请求类型本身没有可绑定的来源字段。
func TestCreateOrderReqExcludesCreatedVia(t *testing.T) {
	if _, ok := reflect.TypeFor[orderdto.CreateOrderReq]().FieldByName("CreatedVia"); ok {
		t.Fatal("请求 DTO 不应携带可信订单来源")
	}
}

// TestCreateOrderUsesAPIEntryPoint 客户端的来源文本不参与服务端选择。
func TestCreateOrderUsesAPIEntryPoint(t *testing.T) {
	cases := []struct {
		name       string
		createdVia string // 空串 = 请求体不带该字段
		withOp     bool
	}{
		{name: "伪造 admin 被覆盖", createdVia: "admin"},
		{name: "伪造 checkout 被覆盖", createdVia: "checkout"},
		{name: "伪造未知值被覆盖", createdVia: "mystery"},
		{name: "不带字段也落 api", createdVia: ""},
		{name: "带后台操作人登录态同样强制 api", createdVia: "admin", withOp: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeCreateOrderService{}
			h := NewHandle(svc)

			payload := map[string]any{
				"projectId":     "proj-1",
				"customerEmail": "customer@example.com",
				"items":         []map[string]any{{"variantId": "v-1", "quantity": 1}},
			}
			if tc.createdVia != "" {
				payload["createdVia"] = tc.createdVia
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("构造请求体失败: %v", err)
			}

			w := postCreateOrder(t, h, string(raw), tc.withOp)
			if w.Code != http.StatusOK {
				t.Fatalf("建单应成功，状态码 %d body=%s", w.Code, w.Body.String())
			}
			if svc.got == nil {
				t.Fatal("service 未被调用，收口无法验证")
			}
			if svc.apiCalls != 1 {
				t.Fatalf("API 建单入口调用 %d 次，期望一次", svc.apiCalls)
			}
			// 收口不得伤及真正由服务端写的审计字段：操作人跟登录态走。
			if tc.withOp && svc.got.CreateBy != 7 {
				t.Fatalf("带操作人时 CreateBy 应为 7，实际 %d", svc.got.CreateBy)
			}
			if !tc.withOp && svc.got.CreateBy != 0 {
				t.Fatalf("匿名调用时 CreateBy 应为 0，实际 %d", svc.got.CreateBy)
			}
		})
	}
}
