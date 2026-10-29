package feature

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	userhttp "go_wp/internal/module/user/inbound/http"
	json "go_wp/pkg/jsonx"
	"go_wp/public/test/support"
)

// 生产迁移与真实访客会话证明订单JSON白名单、归属及申请重试的同一结果。
func TestOrderPublicDataProtocol(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	gin.SetMode(gin.TestMode)
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("Redis 不可用，跳过真实访客会话: %v", err)
	}
	router := gin.New()
	users := userhttp.SetupUserRoutes(router, f.db, f.mail, "测试站")
	orderhttp.SetupOrderPublicRoutes(router, f.orders, userhttp.VisitorIdentityMiddleware(users), userhttp.VisitorCSRFMiddleware())
	_, variant := f.addProduct(t, "订单JSON商品", 23.45, 10)
	orderID, _, itemID := f.paidOrder(t, "order-json-owner@example.com", variant, 3)
	otherOrder, _, _ := f.paidOrder(t, "order-json-other@example.com", variant, 1)
	if err := f.orderModel.UpdateFields(context.Background(), f.projectID, orderID, map[string]any{"admin_note": "后台私有备注", "ip_address": "192.0.2.45", "user_agent": "private-agent"}); err != nil {
		t.Fatal(err)
	}
	cookies := map[string]*http.Cookie{}
	csrf := ""
	request := func(method, path, contentType, body, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "198.51.100.47:3456"
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if token != "" {
			req.Header.Set("X-CSRF-Token", token)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, rec.Code, want, rec.Body.String())
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" {
			t.Fatalf("订单入口必须仅返回JSON: %s", rec.Body.String())
		}
		for _, cookie := range rec.Result().Cookies() {
			cookies[cookie.Name] = cookie
		}
		if token := rec.Header().Get("X-CSRF-Token"); token != "" {
			csrf = token
		}
		if strings.HasPrefix(path, "/api/site/orders") {
			for _, bad := range []string{"adminNote", "costPrice", "ipAddress", "userAgent", "transactionId", "operatorName", "operatorType", "customerEmail", "userId", "totalLabel", "unitPriceLabel", "refundLabel", "statusLabel", "SQLSTATE", "<form", "<div", "后台私有备注", "private-agent"} {
				if strings.Contains(rec.Body.String(), `"`+bad+`":`) || strings.Contains(rec.Body.String(), bad) && (bad == "SQLSTATE" || bad == "<form" || bad == "<div" || bad == "后台私有备注" || bad == "private-agent") {
					t.Fatalf("订单出口泄漏%s: %s", bad, rec.Body.String())
				}
			}
			if want >= 400 {
				var failed struct {
					Message string `json:"message"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &failed); err != nil || failed.Message == "" || strings.Contains(failed.Message, ".err.") {
					t.Fatalf("错误须给出可读业务文案: %s", rec.Body.String())
				}
			}
		}
		return rec
	}
	base := "/api/site/orders?projectId=" + f.projectID
	request("GET", base, "", "", "", 401)
	cookies["gowp_session"] = &http.Cookie{Name: "gowp_session", Value: "admin-session-is-not-visitor"}
	request("GET", base, "", "", "", 401)
	delete(cookies, "gowp_session")
	request("GET", "/api/site/user/session", "", "", "", 200)
	login := func(email string) {
		t.Helper()
		var account, password any
		for _, mail := range f.mail.calls {
			if mail.TemplateKey == "guest_account" && mail.To == email {
				account, password = mail.Vars["username"], mail.Vars["password"]
			}
		}
		if account == nil || password == nil {
			t.Fatal("缺少真实开号邮件夹具")
		}
		body, err := json.Marshal(map[string]any{"account": account, "password": password})
		if err != nil {
			t.Fatal(err)
		}
		request("POST", "/api/site/user/login", "application/json", string(body), csrf, 200)
	}
	login("order-json-owner@example.com")
	listRec := request("GET", base+"&userId=999999", "", "", "", 200)
	if listRec.Header().Get("Cache-Control") != "no-store" || cookies["gowp_user_session"] == nil {
		t.Fatal("订单事实须走独立访客会话且不可缓存")
	}
	var list struct {
		Data orderdto.VisitorOrderListResp `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil || list.Data.Total != 1 || len(list.Data.List) != 1 || list.Data.List[0].ID != orderID {
		t.Fatalf("访客列表归属错误: %+v err=%v", list.Data, err)
	}
	path := func(name string, id uint64, project string) string {
		return fmt.Sprintf("/api/site/orders/%s?projectId=%s&orderId=%d", name, project, id)
	}
	var detail struct {
		Data orderdto.VisitorOrderDetailResp `json:"data"`
	}
	detailRec := request("GET", path("detail", orderID, f.projectID), "", "", "", 200)
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil || detail.Data.Head.ID != list.Data.List[0].ID || detail.Data.Head.Total != list.Data.List[0].Total || detail.Data.Items[0].LineTotal != detail.Data.Head.Total {
		t.Fatalf("列表、详情与商品成交金额应相等: %+v err=%v", detail.Data, err)
	}
	if !strings.Contains(detailRec.Body.String(), `"createTime":1`) || detail.Data.Head.Total != 7035 {
		t.Fatal("金额须为整数分，时间须为原始毫秒")
	}
	var notFound string
	for _, name := range []string{"detail", "returnable", "returns"} {
		foreign := request("GET", path(name, otherOrder, f.projectID), "", "", "", 404)
		missing := request("GET", path(name, orderID, uuid.NewString()), "", "", "", 404)
		if foreign.Body.String() != missing.Body.String() {
			t.Fatal("跨用户与跨工程不能泄漏存在性")
		}
		notFound = foreign.Body.String()
	}
	readAvailable := func() orderdto.ReturnableResp {
		t.Helper()
		rec := request("GET", path("returnable", orderID, f.projectID), "", "", "", 200)
		var payload struct {
			Data orderdto.ReturnableResp `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Data
	}
	before := readAvailable()
	if !before.CanRequest || before.ReturnableTotal != 3 {
		t.Fatal("付费订单的可退事实应由服务提供")
	}
	requestID := uuid.NewString()
	body := fmt.Sprintf(`{"projectId":%q,"orderId":%d,"items":[{"orderItemId":%d,"quantity":1}],"reason":"尺码不合适","requestId":%q,"userId":999999}`, f.projectID, orderID, itemID, requestID)
	request("POST", "/api/site/orders/returns", "application/json", body, "", 403)
	request("POST", "/api/site/orders/returns", "application/json", strings.Replace(body, `"quantity":1`, `"quantity":0`, 1), csrf, 400)
	request("POST", "/api/site/orders/returns", "application/json", strings.Replace(body, `"reason":"尺码不合适"`, `"reason":""`, 1), csrf, 400)
	if after := readAvailable(); after.ReturnableTotal != before.ReturnableTotal {
		t.Fatal("失败申请不得占用可退数量")
	}
	created := request("POST", "/api/site/orders/returns", "application/json", body, csrf, 200)
	var first struct {
		Data orderdto.VisitorReturnResp `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"projectId": {f.projectID}, "orderId": {fmt.Sprint(orderID)}, "orderItemId": {fmt.Sprint(itemID)}, "quantity": {"1"}, "reason": {"尺码不合适"}, "requestId": {requestID}}
	var formBody bytes.Buffer
	writer := multipart.NewWriter(&formBody)
	for key, values := range form {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	replay := request("POST", "/api/site/orders/returns", writer.FormDataContentType(), formBody.String(), csrf, 200)
	var second struct {
		Data orderdto.VisitorReturnResp `json:"data"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &second); err != nil || second.Data.ID != first.Data.ID || second.Data.RefundAmount != first.Data.RefundAmount || second.Data.RefundAmount != detail.Data.Items[0].UnitPrice {
		t.Fatalf("JSON与FormData重试须返回同一申请和服务金额: first=%+v second=%+v err=%v", first.Data, second.Data, err)
	}
	request("POST", "/api/site/orders/returns", "application/json", strings.Replace(body, `"quantity":1`, `"quantity":2`, 1), csrf, 409)
	var returns struct {
		Data orderdto.VisitorReturnListResp `json:"data"`
	}
	if err := json.Unmarshal(request("GET", path("returns", orderID, f.projectID), "", "", "", 200).Body.Bytes(), &returns); err != nil || returns.Data.Total != 1 || returns.Data.List[0].ID != first.Data.ID {
		t.Fatalf("重试只落一张申请: %+v err=%v", returns.Data, err)
	}
	if after := readAvailable(); before.ReturnableTotal-after.ReturnableTotal != first.Data.Items[0].Quantity {
		t.Fatal("可退数量变化须等于唯一申请数量")
	}
	// 不可申请状态的只读事实与写规则来自同一服务口径。
	if err := f.orderModel.UpdateFields(context.Background(), f.projectID, orderID, map[string]any{"status": "pending"}); err != nil {
		t.Fatal(err)
	}
	if readAvailable().CanRequest {
		t.Fatal("未付款不得显示可申请事实")
	}
	request("POST", "/api/site/orders/returns", "application/json", strings.Replace(body, requestID, uuid.NewString(), 1), csrf, 400)
	// 换真实访客会话后，已知请求键也不提供别人的回执。
	cookies = map[string]*http.Cookie{}
	request("GET", "/api/site/user/session", "", "", "", 200)
	login("order-json-other@example.com")
	foreignReplay := request("POST", "/api/site/orders/returns", "application/json", body, csrf, 404)
	if foreignReplay.Body.String() != notFound {
		t.Fatal("已知幂等键不能探测他人申请")
	}
}
