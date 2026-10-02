package orderhttp

// order_page_view_test.go — 订单详情视图的渲染断言（国家/地区的显示形态）。
//
// 判据是**渲染出来的 HTML**：订单详情里的地址是一行拼接文本（orderAddressLabel），
// 国家名换不换得到、语言挑哪一列、字典缺行时会不会把整段地址吞掉 —— 这些在
// 只看 Go 侧 map 时都看不出来，而页面上表现为「地址里少了一截」或「英文界面显示中文」。

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
)

// fakeCountryDict 国家字典替身：只认 CN（按语言给中英名），其余回落代码本身。
type fakeCountryDict struct{ calls int }

func (f *fakeCountryDict) ListDictOptions(context.Context, string) ([]sysconfigdto.DictOption, error) {
	return nil, nil
}

func (f *fakeCountryDict) ListCountryOptions(context.Context, string) ([]sysconfigdto.CountryOption, error) {
	return nil, nil
}

func (f *fakeCountryDict) CountryLabel(_ context.Context, lang, code string) string {
	f.calls++
	if strings.EqualFold(strings.TrimSpace(code), "CN") {
		if strings.HasPrefix(strings.ToLower(lang), "zh") {
			return "中国"
		}
		return "China"
	}
	return strings.TrimSpace(code)
}

// 编译期断言：替身实现契约（契约多一条方法时在这里先失败，而不是等到装配期）。
var _ sysconfigcontract.DictReader = (*fakeCountryDict)(nil)

// requestCtx 造一个带语言参数的请求上下文（response.RequestLanguage 认 lang 查询参数）。
func requestCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/admin/orders?lang="+lang, nil)
	return c
}

// detailResp 一份带国家快照的订单详情（只填地址与状态，其余字段与展示无关）。
func detailResp(country string) *orderdto.OrderDetailResp {
	return &orderdto.OrderDetailResp{
		Head: &orderdto.OrderResp{
			ID: 7, OrderNo: "SO-1", Status: "paid",
			ShipCountry: country, ShipCity: "深圳市", ShipAddress: "某某路 1 号",
			BillCountry: country, BillCity: "深圳市",
		},
	}
}

// TestOrderDetailViewRendersCountryName 详情视图 + 模板渲染：国家代码变成当前语言的名字。
func TestOrderDetailViewRendersCountryName(t *testing.T) {
	dict := &fakeCountryDict{}

	viewOf := func(lang, country string) gin.H {
		return orderDetailView(nil, detailResp(country), orderFilter{}, "p1", 1, 20,
			countryLabelFn(requestCtx(t, lang), dict))
	}

	zh := viewOf("zh-CN", "CN")
	head, ok := zh["Head"].(gin.H)
	if !ok {
		t.Fatalf("详情视图缺少 Head：%+v", zh)
	}
	if got := head["ShippingAddress"].(string); !strings.HasPrefix(got, "中国 深圳市") {
		t.Fatalf("中文界面收货地址应以国家名开头，实际 %q", got)
	}
	if got := head["BillingAddress"].(string); !strings.HasPrefix(got, "中国 深圳市") {
		t.Fatalf("账单地址与收货地址同一口径，实际 %q", got)
	}

	// 真渲染到 orders.html：确认这一行真的进了页面（模板直接输出字符串，
	// 视图里的值再对，模板键写错也照样看不见）。
	data := orderBulkPageData()
	data["HasDetail"] = true
	data["Detail"] = zh
	assertContains(t, renderAdminTemplate(t, "admin/order/orders.html", data), "中国 深圳市")

	// 英文界面取英文列。
	en := viewOf("en-US", "CN")
	if got := en["Head"].(gin.H)["ShippingAddress"].(string); !strings.HasPrefix(got, "China 深圳市") {
		t.Fatalf("英文界面应显示英文国家名，实际 %q", got)
	}

	// 字典里没有这个码 → 回落显示代码（整段地址仍在）。
	unknown := viewOf("zh-CN", "ZZ")
	if got := unknown["Head"].(gin.H)["ShippingAddress"].(string); !strings.HasPrefix(got, "ZZ 深圳市") {
		t.Fatalf("字典缺行时应回落显示代码，实际 %q", got)
	}
	if dict.calls == 0 {
		t.Fatal("字典替身一次都没被调用：说明解析链没有真正接上（视图里显示的可能只是原样的代码）")
	}
}

// TestCountryLabelFnWithoutDict 未接入字典时返回 nil，调用方据此原样显示代码。
func TestCountryLabelFnWithoutDict(t *testing.T) {
	if fn := countryLabelFn(requestCtx(t, "zh-CN"), nil); fn != nil {
		t.Fatal("未注入字典时应返回 nil（applyCountryLabel 据此原样显示代码）")
	}
	// 闭包为 nil 时的回落：原样返回代码（含空值原样返回空）。
	if got := applyCountryLabel(nil, "CN"); got != "CN" {
		t.Fatalf("未接入字典时应显示代码本身，实际 %q", got)
	}
	if got := applyCountryLabel(nil, "  "); got != "" {
		t.Fatalf("空代码应返回空串（不给地址留空段），实际 %q", got)
	}
}
