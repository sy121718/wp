package feature

// order_create_country_test.go — 后台代客建单页的国家/地区（收参归一 → 落库 → 页面下拉与回填）。
//
// 四条判据，每条错了都不报错、只留下不一致的数据或页面：
//   · 合法码（含小写）必须归一化成大写后落库 —— 否则展示层按码取名查不到（页面显示 cn 而不是「中国」）；
//   · 形状不合法的值必须丢弃成空串 —— 列是 VARCHAR(2)，原样进去会让整单落库失败，
//     而运营看到的只会是一句「建单失败」；
//   · 不填时落空串（不是占位符、也不是被悄悄换成默认国家）；
//   · 页面下拉的选项来自字典，且默认选中站点默认国家（预选省掉一次手选）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/pkg/i18n"
)

// orderHeadFromCreate 从建单成功后的提示页回跳链接取订单 id，回读订单头。
//
// 走回跳地址而不是按邮箱之类反查：那条路径本身就是「运营建完单被送回列表页」的真实链路，
// 顺带钉住「回跳地址里必须带 orderId」（少了它，运营看不到刚建的单）。
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

// TestAdminOrderCreateNormalizesCountry 收参：合法码归一化落库，非法值丢弃成空串。
func TestAdminOrderCreateNormalizesCountry(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	_, vid := env.fixture.addProduct(t, "国家建单", 30, 5)

	form := adminOrderCreateForm(env.project, vid, "country@example.com", "req-country-1")
	form.Set("shipCountry", "cn")    // 小写：落库前必须归一化成 CN
	form.Set("billCountry", "China") // 三个字母以上：列装不下，必须丢弃
	head := orderHeadFromCreate(t, env, env.post(t, form))

	if head.ShipCountry != "CN" {
		t.Fatalf("小写国家码应归一化成 CN 落库，实际 %q", head.ShipCountry)
	}
	if head.BillCountry != "" {
		t.Fatalf("形状不合法的国家码应丢弃成空串，实际 %q", head.BillCountry)
	}
}

// TestAdminOrderCreateCountryOptional 不填国家时落空串（不是占位符，也不是被换成默认国家）。
func TestAdminOrderCreateCountryOptional(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	_, vid := env.fixture.addProduct(t, "无国家建单", 30, 5)

	// 表单里没有 shipCountry / billCountry（浏览器在选中「（不填写）」时就是这样提交的）。
	form := adminOrderCreateForm(env.project, vid, "nocountry@example.com", "req-country-2")
	head := orderHeadFromCreate(t, env, env.post(t, form))

	if head.ShipCountry != "" || head.BillCountry != "" {
		t.Fatalf("不填国家时应落空串，实际 %q / %q", head.ShipCountry, head.BillCountry)
	}
}

// TestAdminOrderCreatePageRendersCountrySelect 页面下拉：选项来自字典，默认选中站点默认国家。
//
// 断言的是**渲染出来的 HTML**：Jet 缺键会整页 500、键写错会让下拉静默消失，
// 而两者在 Go 侧看数据都对。
func TestAdminOrderCreatePageRendersCountrySelect(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	page := env.get(t, "/admin/orders/new?project="+env.project)

	for _, want := range []string{
		`name="shipCountry"`,
		`name="billCountry"`,
		// 默认项被选中：期望值取自与 handler 同一个函数（本站默认国家）。
		// 这里验的是「Selected 标志真的落到了 HTML 上」—— 写成写死 CN 会在
		// 换默认国家的站点上误报，而这条断言关心的是渲染管道通不通。
		`<option value="` + i18n.GetDefaultCountry() + `" selected>`,
		// 首位空项常驻（国家是选填）。
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

	// 把命中的那几行真 HTML 打出来（-v 可见）：断言失败时能一眼看出渲染成了什么样，
	// 成功时它就是「下拉确实在页面里、默认项确实被选中」的原始证据。
	// 命中处只截一小段：整行是两百多个 <option>，全打出来反而看不见重点。
	wantSelected := `<option value="` + i18n.GetDefaultCountry() + `" selected`
	t.Logf("渲染片段：%s", htmlAround(page, `name="shipCountry"`, 70))
	t.Logf("渲染片段：%s", htmlAround(page, wantSelected, 60))
}

// htmlAround 取 needle 在 page 里首次出现处前后 pad 个字符（找不到时返回提示串）。
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

// TestAdminOrderCreateEchoesCountryOnFailure 失败重渲时国家选择不丢（与其余字段同一档）。
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
	// 回填的是**提交值**：重渲后运营看到的仍是自己刚选的国家，而不是回到默认值。
	for _, want := range []string{
		`<option value="US" selected>`,
		`<option value="JP" selected>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("失败重渲丢了国家选择：缺少 %q", want)
		}
	}
}
