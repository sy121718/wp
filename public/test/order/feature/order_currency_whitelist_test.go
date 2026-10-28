package feature

// order_currency_whitelist_test.go — 建单币种白名单（迁移 633 的补口，词条见迁移 675）。
//
// 为什么必须有这条校验：币种决定汇率快照能不能成立。非法币种不报错的话
// `orders.fx_rate` 会静默留 NULL，那张单在销售报表里与「这个币对还没配汇率」
// **完全同形** —— 运营分不出「币种根本不存在」与「汇率没配」。
//
// 会红断言：
//
//	1  字典里没有的币种 → 拒绝，且不扣库存、不落单
//	2  字典里有的币种（小写也认）→ 放过，落库归一成大写
//	3  空币种（= 用站点默认币）→ 放过（默认值本来就来自系统配置）
//	4  字典读不到 / 未注入 → 放过（基础设施问题不该表现成「客户下不了单」）
//	5  建单页真的渲染出币种下拉，默认项选中站点默认币
//	6  页面提交非法币种 → 200 就地重渲 + 一句人话（不是「系统内部错误」）

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

// withCurrencyDict 给夹具的订单 service 接上真实字典（sysconfig service + 适配器）。
//
// 用真字典而不是替身：白名单的数据源就是迁移 seed 的 178 行 ISO 4217，
// 替身要把它再抄一份进测试，抄错的那份不会报错。
func (f *orderFixture) withCurrencyDict() {
	f.orders.SetCurrencyLister(
		currencydict.New(sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(f.db), nil)))
}

// TestCreateOrderRejectsUnknownCurrency 验收 1 / 2 / 3。
func TestCreateOrderRejectsUnknownCurrency(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	f.withCurrencyDict()
	ctx := context.Background()
	_, vid := f.addProduct(t, "币种校验", 100, 10)
	before := f.stockOf(t, vid)

	// ① 字典里没有的币种：拒绝，且什么都没变。
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

	// ② 字典里有的币种：放过（小写也认，落库归一成大写）。
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

	// ③ 空币种 = 用站点默认币：放过，落站点默认币。
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

// TestCreateOrderSkipsCurrencyCheckWithoutDict 验收 4：字典没接上时放行。
//
// 反向断言的意义：把「字典读不到」写成拒绝，故障形态会变成「全站外币单下不了」，
// 而真因只是装配缺一个端口或一次查询失败。
func TestCreateOrderSkipsCurrencyCheckWithoutDict(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	// 不调 withCurrencyDict：与「字典未注入」同形。
	_, vid := f.addProduct(t, "无字典", 100, 5)

	req := f.createBaseReq(vid, 1)
	req.Currency = "ZZZ"
	if _, err := f.orders.CreateOrder(context.Background(), req); err != nil {
		t.Fatalf("未接字典时不该拦单，实际 %v", err)
	}
}

// TestAdminOrderCreatePageRendersCurrencySelect 验收 5。
//
// 断言渲染出来的 HTML：数据都对的 Go 侧看不出「下拉没渲染」这种失败。
func TestAdminOrderCreatePageRendersCurrencySelect(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	page := env.get(t, "/admin/orders/new?project="+env.project)

	for _, want := range []string{
		`name="currency"`,
		`id="order-new-currency"`,
		// 首位是「用站点默认币」：空 value 而不是缺项。
		`<option value="">`,
		// 站点默认币那一项被选中（期望值取自同一份配置，写死 CNY 会在换默认币的站点误报）。
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

// TestAdminOrderCreatePageRejectsUnknownCurrency 验收 6：非法币种 → 200 重渲 + 人话。
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
	// 词条在迁移 675 里 seed；feature 测试不加载 i18n 缓存，于是出口给的是 key
	// （tr(key, fallback) 未命中时原样返回）。两者居其一即「这句话到了页面上」——
	// 写死中文会在英文站或词条改名后误报，而这条断言关心的是通道通不通。
	if !strings.Contains(out, orderenums.ErrCurrencyUnknown) &&
		!strings.Contains(out, "不在系统货币字典里") {
		t.Errorf("页面应给出币种被拒的文案（key 或兜底），实际渲染里都没有")
	}
	if strings.Contains(out, "系统内部错误") {
		t.Errorf("业务拒绝被归口成了系统故障")
	}
	// 回填：运营刚选的币种还在（不是悄悄回到默认项）。
	if !strings.Contains(out, `<option value="ZZZ" selected>`) {
		t.Errorf("失败重渲丢了币种选择")
	}
	if got := env.fixture.orderCount(t); got != 0 {
		t.Fatalf("被拒的提交不该落单，实际 %d 张", got)
	}
}
