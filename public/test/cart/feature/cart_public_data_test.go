package feature

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	carthttp "go_wp/internal/module/cart/inbound/http"
	orderdto "go_wp/internal/module/order/dto"
	userhttp "go_wp/internal/module/user/inbound/http"
	"go_wp/pkg/auth"
	json "go_wp/pkg/jsonx"
)

func TestCartPublicDataProtocol(t *testing.T) {
	f := newCartFixtureWithGateway(t, failingGateway{})
	if f == nil {
		return
	}
	gin.SetMode(gin.TestMode)
	cfg := viper.New()
	cfg.Set("auth.session_secret", "cart-public-data-test-cookie-secret")
	if err := auth.Init(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = auth.Close() })
	router := gin.New()
	users := userhttp.SetupUserRoutes(router, f.db, f.mail, "测试站")
	carthttp.SetupCartPublicRoutes(router, f.cart, userhttp.VisitorIdentityMiddleware(users), userhttp.VisitorCSRFMiddleware())
	_, variant := f.addProduct(t, "纯数据商品", 12.34, 10)
	cookies := map[string]*http.Cookie{}
	csrf := ""
	request := func(method, path, contentType, body, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "203.0.113.8:3456"
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
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("API不是JSON: %s", rec.Body.String())
		}
		if rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" {
			t.Fatal("JSON API不能决定界面导航")
		}
		for _, cookie := range rec.Result().Cookies() {
			cookies[cookie.Name] = cookie
		}
		if value := rec.Header().Get("X-CSRF-Token"); value != "" {
			csrf = value
		}
		for _, bad := range []string{"\"cookie\"", "\"Cookie\"", "totalLabel", "AvailableText", "<form", "<div", "costPrice", "SQLSTATE"} {
			if strings.Contains(rec.Body.String(), bad) {
				t.Fatalf("泄漏展示或内部字段%s: %s", bad, rec.Body.String())
			}
		}
		return rec
	}
	read := request(http.MethodGet, "/api/site/cart?projectId="+f.projectID, "", "", "", 200)
	if csrf == "" || cookies["gowp_user_session"] == nil || cookies["gowp_session"] != nil {
		t.Fatal("访客令牌必须来自独立cookie域")
	}
	var snapshot struct {
		Data cartdto.CartSnapshot `json:"data"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.Data.Empty || snapshot.Data.Total != 0 || len(snapshot.Data.Items) != 0 {
		t.Fatalf("空车事实错误:%+v", snapshot.Data)
	}
	addBody := `{"projectId":"` + f.projectID + `","variantId":"` + variant + `","quantity":2,"cookie":"attacker","total":1}`
	request(http.MethodPost, "/api/site/cart/add", "application/json", addBody, "", 403)
	request(http.MethodPost, "/api/site/cart/add", "application/json", addBody, "admin-token", 403)
	added := request(http.MethodPost, "/api/site/cart/add", "application/json", addBody, csrf, 200)
	if err := json.Unmarshal(added.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Data.Total != 2468 || snapshot.Data.ItemCount != 2 || len(snapshot.Data.Items) != 1 || snapshot.Data.Items[0].UnitPrice != 1234 || snapshot.Data.Items[0].Available == nil {
		t.Fatalf("金额和库存应来自服务:%+v", snapshot.Data)
	}
	cartCookie := cookies[cartcontract.CartCookieName]
	if cartCookie == nil || !cartCookie.HttpOnly || cartCookie.SameSite != http.SameSiteLaxMode || cartCookie.Path != "/" {
		t.Fatalf("购物车cookie安全属性不正确:%+v", cartCookie)
	}
	request(http.MethodPost, "/api/site/cart/quantity", "application/json", `{"projectId":"`+f.projectID+`","variantId":"`+variant+`"}`, csrf, 400)

	unchanged := request(http.MethodGet, "/api/site/cart?projectId="+f.projectID, "", "", "", 200)
	if err := json.Unmarshal(unchanged.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Data.ItemCount != 2 {
		t.Fatal("失败请求改变了购物车")
	}
	request(http.MethodPost, "/api/site/cart/add", "application/json", strings.Replace(addBody, f.projectID, uuid.NewString(), 1), csrf, 404)

	values := url.Values{"projectId": {f.projectID}, "email": {"json-buyer@example.com"}, "name": {"数据访客"}, "phone": {"13800000000"}, "address": {"测试地址"}, "country": {"CN"}, "requestId": {"public-data-checkout"}, "userId": {"987654321"}, "ipAddress": {"attacker"}, "cookie": {"attacker"}}
	values.Del("address")
	request(http.MethodPost, "/api/site/cart/checkout", "application/x-www-form-urlencoded", values.Encode(), csrf, 400)
	before, err := f.orders.ListOrders(context.Background(), &orderdto.ListOrderReq{ProjectID: f.projectID, Limit: 50})
	if err != nil || before.Total != 0 || f.reservedOf(t, variant) != 0 {
		t.Fatalf("校验失败不得建单或预占库存: orders=%+v err=%v", before, err)
	}
	values.Set("address", "测试地址")

	checked := request(http.MethodPost, "/api/site/cart/checkout", "application/x-www-form-urlencoded", values.Encode(), csrf, 200)
	var checkout struct {
		Data cartdto.CheckoutResp `json:"data"`
	}
	if err := json.Unmarshal(checked.Body.Bytes(), &checkout); err != nil {
		t.Fatal(err)
	}
	if checkout.Data.Paid || checkout.Data.OrderNo == "" || checkout.Data.Total != 2468 || checkout.Data.Status != "pending" || checkout.Data.PaymentError == "" {
		t.Fatalf("支付失败仍须返回已建单事实:%+v", checkout.Data)
	}
	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: checkout.Data.OrderID})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Head.ShipName != "数据访客" || detail.Head.ShipPhone != "13800000000" || detail.Head.IPAddress == "attacker" || (detail.Head.UserID != nil && *detail.Head.UserID == 987654321) {
		t.Fatalf("可信元数据或收货缺省错误:%+v", detail.Head)
	}
	if detail.Head.PaidAt != nil {
		t.Fatal("支付失败不得写入付款时间")
	}

	jsonBody := `{"projectId":"` + f.projectID + `","email":"json-buyer@example.com","name":"数据访客","phone":"13800000000","shipping":{"address":"测试地址","country":"CN"},"requestId":"public-data-checkout"}`
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	for key, entries := range values {
		for _, value := range entries {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, retry := range []struct{ name, contentType, body string }{
		{"JSON", "application/json", jsonBody},
		{"FormData", writer.FormDataContentType(), multipartBody.String()},
	} {
		cookies[cartcontract.CartCookieName] = cartCookie
		replayed := request(http.MethodPost, "/api/site/cart/checkout", retry.contentType, retry.body, csrf, 200)
		var same struct {
			Data cartdto.CheckoutResp `json:"data"`
		}
		if err := json.Unmarshal(replayed.Body.Bytes(), &same); err != nil {
			t.Fatal(err)
		}
		if same.Data.OrderID != checkout.Data.OrderID || same.Data.OrderNo != checkout.Data.OrderNo || same.Data.Total != checkout.Data.Total || same.Data.Currency != checkout.Data.Currency || same.Data.Paid != checkout.Data.Paid || same.Data.Status != checkout.Data.Status || same.Data.PaymentError != checkout.Data.PaymentError {
			t.Fatalf("%s 重试与首次订单事实不等: first=%+v retry=%+v", retry.name, checkout.Data, same.Data)
		}
	}
	list, err := f.orders.ListOrders(context.Background(), &orderdto.ListOrderReq{ProjectID: f.projectID, Limit: 50})
	if err != nil || list.Total != 1 {
		t.Fatalf("多种形状重试只能创建同一单: %+v err=%v", list, err)
	}
	if got := f.reservedOf(t, variant); got != snapshot.Data.ItemCount {
		t.Fatalf("库存预占必须等于首次购物车件数: reserved=%d cart=%d", got, snapshot.Data.ItemCount)
	}
	final := request(http.MethodGet, "/api/site/cart?projectId="+f.projectID, "", "", "", 200)
	if err := json.Unmarshal(final.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.Data.Empty {
		t.Fatal("订单建成后应清空购物车")
	}
}
