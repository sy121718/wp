package feature

import (
	"context"
	"strings"
	"testing"

	orderenums "go_wp/internal/module/order/enums"
	"go_wp/internal/module/order/outbound/currencydict"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	"go_wp/pkg/i18n"
)

func (f *orderFixture) withCurrencyDict() {
	f.orders.SetCurrencyLister(
		currencydict.New(sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(f.db), nil)))
}

func TestCreateOrderRejectsUnknownCurrency(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	f.withCurrencyDict()
	ctx := context.Background()
	_, vid := f.addProduct(t, "币种校验", 100, 10)
	before := f.stockOf(t, vid)

	bad := f.createBaseReq(vid, 1)
	bad.Currency = "ZZZ"
	if _, err := f.orders.CreateOrder(ctx, bad); err == nil {
		t.Fatalf("字典里没有的币种应被拒")
	} else if !strings.Contains(err.Error(), orderenums.ErrCurrencyUnknown) {
		t.Fatalf("应返回 ErrCurrencyUnknown，实际 %v", err)
	}
	if got := f.orderCount(t); got != 0 {
		t.Fatalf("币种被拒时不该落单，实际 %d 张", got)
	}
	if got := f.stockOf(t, vid); got != before {
		t.Fatalf("币种被拒时不该动库存：前 %d，后 %d", before, got)
	}

	good := f.createBaseReq(vid, 1)
	good.Currency = "usd"
	res, err := f.orders.CreateOrder(ctx, good)
	if err != nil {
		t.Fatalf("合法币种建单失败: %v", err)
	}
	var stored string
	if qerr := f.db.Raw("SELECT currency FROM orders WHERE id = ?", res.ID).Row().Scan(&stored); qerr != nil {
		t.Fatalf("读订单币种失败: %v", qerr)
	}
	if stored != "USD" {
		t.Fatalf("币种应归一成大写落库，实际 %q", stored)
	}

	plain := f.createBaseReq(vid, 1)
	plainRes, err := f.orders.CreateOrder(ctx, plain)
	if err != nil {
		t.Fatalf("空币种建单失败: %v", err)
	}
	var defaulted string
	if qerr := f.db.Raw("SELECT currency FROM orders WHERE id = ?", plainRes.ID).Row().Scan(&defaulted); qerr != nil {
		t.Fatalf("读订单币种失败: %v", qerr)
	}
	if defaulted != i18n.GetDefaultCurrency() {
		t.Fatalf("空币种应落站点默认币 %q，实际 %q", i18n.GetDefaultCurrency(), defaulted)
	}
}

func TestCreateOrderSkipsCurrencyCheckWithoutDict(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}

	_, vid := f.addProduct(t, "无字典", 100, 5)

	req := f.createBaseReq(vid, 1)
	req.Currency = "ZZZ"
	if _, err := f.orders.CreateOrder(context.Background(), req); err != nil {
		t.Fatalf("未接字典时不该拦单，实际 %v", err)
	}
}

func TestAdminOrderCreatePageRendersCurrencySelect(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	page := env.get(t, "/admin/orders/new?project="+env.project)

	for _, want := range []string{
		`name="currency"`,
		`id="order-new-currency"`,

		`<option value="">`,

		`<option value="` + i18n.GetDefaultCurrency() + `" selected>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("建单页缺少 %q", want)
		}
	}
	if got := strings.Count(page, `name="currency"`); got != 1 {
		t.Errorf("币种下拉应恰好一个，实际 %d", got)
	}
}

func TestAdminOrderCreatePageRejectsUnknownCurrency(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	env.fixture.withCurrencyDict()
	_, vid := env.fixture.addProduct(t, "页面币种", 30, 5)

	form := adminOrderCreateForm(env.project, vid, "currency@example.com", "req-currency-1")
	form.Set("currency", "ZZZ")
	rec := env.post(t, form)

	if rec.Code != 200 {
		t.Fatalf("业务拒绝必须就地重渲（200），实际 %d", rec.Code)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "</html>") {
		t.Fatal("失败重渲的输出不是一整页（缺 </html>）")
	}

	if !strings.Contains(out, orderenums.ErrCurrencyUnknown) &&
		!strings.Contains(out, "不在系统货币字典里") {
		t.Errorf("页面应给出币种被拒的文案（key 或兜底），实际渲染里都没有")
	}
	if strings.Contains(out, "系统内部错误") {
		t.Errorf("业务拒绝被归口成了系统故障")
	}

	if !strings.Contains(out, `<option value="ZZZ" selected>`) {
		t.Errorf("失败重渲丢了币种选择")
	}
	if got := env.fixture.orderCount(t); got != 0 {
		t.Fatalf("被拒的提交不该落单，实际 %d 张", got)
	}
}
