package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/pkg/i18n"
)

func orderHeadFromCreate(t *testing.T, env *orderCreatePageEnv, rec *httptest.ResponseRecorder) *orderdto.OrderResp {
	t.Helper()
	id := orderIDFromCreate(t, rec)
	detail, err := env.fixture.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{
		ProjectID: env.project, OrderID: id,
	})
	if err != nil {
		t.Fatalf("回读订单失败：%v", err)
	}
	return detail.Head
}

func TestAdminOrderCreateNormalizesCountry(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	_, vid := env.fixture.addProduct(t, "国家建单", 30, 5)

	form := adminOrderCreateForm(env.project, vid, "country@example.com", "req-country-1")
	form.Set("shipCountry", "cn")
	form.Set("billCountry", "China")
	head := orderHeadFromCreate(t, env, env.post(t, form))

	if head.ShipCountry != "CN" {
		t.Fatalf("小写国家码应归一化成 CN 落库，实际 %q", head.ShipCountry)
	}
	if head.BillCountry != "" {
		t.Fatalf("形状不合法的国家码应丢弃成空串，实际 %q", head.BillCountry)
	}
}

func TestAdminOrderCreateCountryOptional(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	_, vid := env.fixture.addProduct(t, "无国家建单", 30, 5)

	form := adminOrderCreateForm(env.project, vid, "nocountry@example.com", "req-country-2")
	head := orderHeadFromCreate(t, env, env.post(t, form))

	if head.ShipCountry != "" || head.BillCountry != "" {
		t.Fatalf("不填国家时应落空串，实际 %q / %q", head.ShipCountry, head.BillCountry)
	}
}

func TestAdminOrderCreatePageRendersCountrySelect(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	page := env.get(t, "/admin/orders/new?project="+env.project)

	for _, want := range []string{
		`name="shipCountry"`,
		`name="billCountry"`,

		`<option value="` + i18n.GetDefaultCountry() + `" selected>`,

		`<option value="">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("建单页缺少 %q", want)
		}
	}
	if got := strings.Count(page, `name="shipCountry"`); got != 1 {
		t.Errorf("收货国家下拉应恰好一个，实际 %d", got)
	}
	if got := strings.Count(page, `name="billCountry"`); got != 1 {
		t.Errorf("账单国家下拉应恰好一个，实际 %d", got)
	}

	wantSelected := `<option value="` + i18n.GetDefaultCountry() + `" selected`
	t.Logf("渲染片段：%s", htmlAround(page, `name="shipCountry"`, 70))
	t.Logf("渲染片段：%s", htmlAround(page, wantSelected, 60))
}

func htmlAround(page, needle string, pad int) string {
	idx := strings.Index(page, needle)
	if idx < 0 {
		return "<未出现：" + needle + ">"
	}
	start, end := idx-pad, idx+len(needle)+pad
	if start < 0 {
		start = 0
	}
	if end > len(page) {
		end = len(page)
	}
	return strings.TrimSpace(strings.ReplaceAll(page[start:end], "\n", " "))
}

func TestAdminOrderCreateEchoesCountryOnFailure(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	_, vid := env.fixture.addProduct(t, "国家回填", 30, 5)

	form := adminOrderCreateForm(env.project, vid, "not-an-email", "req-country-3")
	form.Set("shipCountry", "US")
	form.Set("billCountry", "JP")
	rec := env.post(t, form)

	if rec.Code != http.StatusOK {
		t.Fatalf("失败提交必须就地重渲（200），实际 %d", rec.Code)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "</html>") {
		t.Fatal("失败重渲的输出不是一整页（缺 </html>）")
	}

	for _, want := range []string{
		`<option value="US" selected>`,
		`<option value="JP" selected>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("失败重渲丢了国家选择：缺少 %q", want)
		}
	}
}
