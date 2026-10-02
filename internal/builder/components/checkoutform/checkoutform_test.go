package checkoutform

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	ordercontract "go_wp/internal/module/order/contract"
)

// testContext 一份「装配完整」的构建上下文（站点工程 + 国家清单 + 语言）。
func testContext() *core.RenderContext {
	return &core.RenderContext{
		ProjectID: "p-42",
		Lang:      "en-US",
		Checkout: core.CheckoutInput{
			Countries:      []core.CheckoutCountry{{Code: "CN", Label: "China"}, {Code: "US", Label: "United States"}},
			DefaultCountry: "CN",
		},
	}
}

// mustRequired 取清单里所有必填字段的 key（构造合法用例时用）。
func mustRequired() []Field {
	fields := []Field{}
	for _, f := range ordercontract.CheckoutFormFields() {
		if f.Required {
			fields = append(fields, Field{Key: f.Key})
		}
	}
	return fields
}

// fullSelection 清单里全部可进表单字段的选择（构造合法用例时用）。
func fullSelection() []Field {
	fields := []Field{}
	for _, f := range ordercontract.CheckoutFormFields() {
		fields = append(fields, Field{Key: f.Key})
	}
	return fields
}

// withFields 在必填字段的基础上追加若干字段（保持必填齐全）。
func withFields(extra ...Field) []Field {
	return append(mustRequired(), extra...)
}

// TestValidateExtra 校验：非法 key、重复、必填不可缺、长度与数量上限。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空字段列表（用默认勾选）合法", &Props{}, false},
		{"仅必填字段合法", &Props{Fields: mustRequired()}, false},
		{"全部字段合法", &Props{Fields: fullSelection()}, false},
		{"非法 key 拒绝", &Props{Fields: withFields(Field{Key: "nickname"})}, true},
		{"大小写不合法的 key 拒绝", &Props{Fields: withFields(Field{Key: "Email"})}, true},
		{"空 key 拒绝", &Props{Fields: withFields(Field{Key: ""})}, true},
		{"系统字段（不进表单）拒绝", &Props{Fields: withFields(Field{Key: "requestId"})}, true},
		{"重复 key 拒绝", &Props{Fields: withFields(Field{Key: "province"}, Field{Key: "province"})}, true},
		{"必填字段缺失拒绝", &Props{Fields: []Field{{Key: "email"}, {Key: "name"}, {Key: "phone"}}}, true},
		{"标签超长拒绝", &Props{Fields: withFields(Field{Key: "province", Label: strings.Repeat("字", maxLabelLen+1)})}, true},
		{"占位提示超长拒绝", &Props{Fields: withFields(Field{Key: "province", Placeholder: strings.Repeat("字", maxPlaceholderLen+1)})}, true},
		{"提交文字超长拒绝", &Props{SubmitLabel: strings.Repeat("字", maxSubmitLabelLen+1)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtra(tt.props, "cf1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExtra(%+v) err=%v, wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

// TestValidateExtraFieldCountLimit 字段数量上限（契约只有 20 个可选项，上限 32 是防滥用）。
func TestValidateExtraFieldCountLimit(t *testing.T) {
	fields := mustRequired()
	for len(fields) < maxFields+1 {
		// 用重复 key 也不影响本用例的判据：数量检查排在前面的循环之前会先命中，
		// 这里要断言的是「超量被拒」，具体原因不参与判定。
		fields = append(fields, Field{Key: "zip"})
	}
	if err := validateExtra(&Props{Fields: fields}, "cf1"); err == nil {
		t.Fatalf("字段数 %d 应被拒绝（上限 %d）", len(fields), maxFields)
	}
}

// TestResolveSelectionDefaults 空 Fields = 清单里 DefaultOn 的那一套，且必填项齐全。
func TestResolveSelectionDefaults(t *testing.T) {
	selected, err := resolveSelection(&Props{})
	if err != nil {
		t.Fatalf("resolveSelection: %v", err)
	}
	got := map[string]bool{}
	for _, f := range selected {
		got[f.Key] = true
	}
	for _, f := range ordercontract.CheckoutFormFields() {
		if f.Required && !got[f.Key] {
			t.Errorf("默认选择缺少必填字段 %q", f.Key)
		}
		if f.DefaultOn && !got[f.Key] {
			t.Errorf("默认选择缺少默认勾选字段 %q", f.Key)
		}
	}
	// 收货人 / 收货电话默认不勾（cart.go 的缺省就是联系人姓名与电话）。
	for _, key := range []string{"shipName", "shipPhone"} {
		if got[key] {
			t.Errorf("默认选择不该包含 %q（缺省沿用联系人）", key)
		}
	}
}

// TestBuildViewWithoutProjectID 缺站点工程 → 提示视图，不报错（编辑器画布 / 组件库契约测试
// 本来就没有工程上下文，报错会让「每个组件插入后都能编译」这条既有契约失效）。
func TestBuildViewWithoutProjectID(t *testing.T) {
	view, err := BuildView(&Props{}, &core.RenderContext{})
	if err != nil {
		t.Fatalf("缺工程 id 不该报错，得到: %v", err)
	}
	if view.Notice == "" {
		t.Fatal("缺工程 id 应渲染不可用提示")
	}
	if len(view.Groups) != 0 {
		t.Fatalf("提示分支不该渲染字段，得到 %d 组", len(view.Groups))
	}
	attrs, classes := view.DeclareFeatures()
	if len(attrs) != 0 || len(classes) != 0 {
		t.Fatalf("提示分支不该登记任何运行时特征，得到 %v / %v", attrs, classes)
	}
}

// TestBuildViewMissingCountries 表单里有国家字段但构建期没注入国家清单 → 明确报错。
//
// 这是「静默降级 vs 构建失败」的判据所在：降级成「只有一个默认国家的下拉」会把访客
// 锁死在一个国家，而页面上完全看不出异常。
func TestBuildViewMissingCountries(t *testing.T) {
	ctx := &core.RenderContext{ProjectID: "p-1"}
	_, err := BuildView(&Props{Fields: withFields(Field{Key: "country"})}, ctx)
	if err == nil {
		t.Fatal("表单含国家字段、未注入国家清单时应报错")
	}
	if !strings.Contains(err.Error(), "国家") {
		t.Fatalf("错误信息应点明国家清单缺失，得到: %v", err)
	}
}

// TestBuildViewNoCountryFieldNeedsNoCountries 表单没有国家字段时不检查国家清单：
// 不需要的数据缺失不构成缺陷（非中国站点会把国家关掉）。
func TestBuildViewNoCountryFieldNeedsNoCountries(t *testing.T) {
	ctx := &core.RenderContext{ProjectID: "p-1"}
	fields := []Field{}
	for _, f := range ordercontract.CheckoutFormFields() {
		if f.Type == ordercontract.CheckoutFieldCountry {
			continue
		}
		fields = append(fields, Field{Key: f.Key})
	}
	if _, err := BuildView(&Props{Fields: fields}, ctx); err != nil {
		t.Fatalf("不含国家字段时不该因缺国家清单报错: %v", err)
	}
}

// TestBuildViewGroupsOrderAndBillingSwitch 分组、字段顺序与账单开关。
func TestBuildViewGroupsOrderAndBillingSwitch(t *testing.T) {
	ctx := &core.RenderContext{
		ProjectID: "p-1",
		Lang:      "zh-CN",
		Checkout: core.CheckoutInput{
			Countries:      []core.CheckoutCountry{{Code: "CN", Label: "中国"}, {Code: "US", Label: "美国"}},
			DefaultCountry: "CN",
		},
	}
	fields := withFields(Field{Key: "country"}, Field{Key: "billAddress"})

	// 不开账单：字段顺序按作者配置，账单组整组不渲染。
	view, err := BuildView(&Props{Fields: fields}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Groups) == 0 {
		t.Fatal("应至少有一个分组")
	}
	if got := view.Groups[0].Key; got != ordercontract.CheckoutGroupContact {
		t.Fatalf("首个分组应为联系信息，得到 %q", got)
	}
	for _, g := range view.Groups {
		if g.Key == ordercontract.CheckoutGroupBilling {
			t.Fatalf("未开账单地址时不该渲染账单组: %+v", g)
		}
	}
	// 顺序：作者配置里 country 排在必填项之后 → 收货组里 country 必须紧跟其后出现。
	shipping := groupOf(view.Groups, ordercontract.CheckoutGroupShipping)
	if shipping == nil || len(shipping.Fields) == 0 {
		t.Fatal("应有收货地址分组")
	}
	if shipping.Fields[0].Key != "address" || shipping.Fields[1].Key != "country" {
		t.Fatalf("收货组应按作者配置的顺序渲染（address → country）: %+v", shipping.Fields)
	}

	// 开账单：账单组出现。
	view, err = BuildView(&Props{Fields: fields, CollectBilling: true}, ctx)
	if err != nil {
		t.Fatalf("BuildView(collectBilling): %v", err)
	}
	if groupOf(view.Groups, ordercontract.CheckoutGroupBilling) == nil {
		t.Fatal("开启账单地址后应渲染账单组")
	}
}

// TestBuildViewCountryOptionSelected 默认国家预选中；字典里没有它时全不选中（浏览器取第一项）。
func TestBuildViewCountryOptionSelected(t *testing.T) {
	ctx := &core.RenderContext{
		ProjectID: "p-1",
		Checkout: core.CheckoutInput{
			Countries:      []core.CheckoutCountry{{Code: "CN", Label: "中国"}, {Code: "US", Label: "美国"}},
			DefaultCountry: "us", // 大小写不敏感
		},
	}
	view, err := BuildView(&Props{Fields: withFields(Field{Key: "country"})}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	var country *FieldView
	for i := range view.Groups {
		for j := range view.Groups[i].Fields {
			if view.Groups[i].Fields[j].Key == "country" {
				country = &view.Groups[i].Fields[j]
			}
		}
	}
	if country == nil {
		t.Fatal("找不到国家字段")
	}
	if len(country.Options) != 2 {
		t.Fatalf("国家选项数量应为 2，得到 %d", len(country.Options))
	}
	if country.Options[0].Selected || !country.Options[1].Selected {
		t.Fatalf("默认国家应为 US（大小写不敏感）: %+v", country.Options)
	}

	ctx.Checkout.DefaultCountry = "ZZ"
	view, err = BuildView(&Props{Fields: withFields(Field{Key: "country"})}, ctx)
	if err != nil {
		t.Fatalf("BuildView(未知默认国家): %v", err)
	}
	for _, g := range view.Groups {
		for _, f := range g.Fields {
			for _, o := range f.Options {
				if o.Selected {
					t.Fatalf("默认国家不在字典里时不该预选任何项: %+v", o)
				}
			}
		}
	}
}

// TestBuildViewRequiredCanOnlyTighten 必填只能加严：清单必填项恒为 required，作者可把可选字段设为必填。
func TestBuildViewRequiredCanOnlyTighten(t *testing.T) {
	ctx := &core.RenderContext{ProjectID: "p-1"}
	fields := withFields(Field{Key: "zip", Required: true})
	for i := range fields {
		if fields[i].Key == "email" {
			fields[i].Required = false // 试图放松清单必填项
		}
	}
	view, err := BuildView(&Props{Fields: fields}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	required := map[string]bool{}
	for _, g := range view.Groups {
		for _, f := range g.Fields {
			required[f.Key] = f.Required
		}
	}
	if !required["email"] {
		t.Fatal("清单必填项不能被放松（email 仍应为 required）")
	}
	if !required["zip"] {
		t.Fatal("作者可以把可选字段加严为必填（zip）")
	}
}

// TestBuildViewHiddenAndAction projectId / lang 进隐藏字段（片段端 POST 只读表单体）。
func TestBuildViewHiddenAndAction(t *testing.T) {
	ctx := testContext()
	view, err := BuildView(&Props{}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Action != FragmentPath {
		t.Fatalf("提交地址应为 %q，得到 %q", FragmentPath, view.Action)
	}
	got := map[string]string{}
	for _, h := range view.Hidden {
		got[h.Name] = h.Value
	}
	if got["projectId"] != "p-42" || got["lang"] != "en-US" {
		t.Fatalf("隐藏字段异常: %+v", view.Hidden)
	}
	if view.SubmitLabel != DefaultSubmitLabel {
		t.Fatalf("缺省提交文字应为 %q，得到 %q", DefaultSubmitLabel, view.SubmitLabel)
	}
}

// TestDeclareFeatures 表单分支登记 hx-* 三个属性（产物据此注入 htmx 与片段基座样式）。
func TestDeclareFeatures(t *testing.T) {
	ctx := testContext()
	view, err := BuildView(&Props{}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	attrs, classes := view.DeclareFeatures()
	for _, want := range []string{"hx-post", "hx-target", "hx-swap"} {
		if !contains(attrs, want) {
			t.Errorf("表单分支应登记 %q，得到 %v", want, attrs)
		}
	}
	if len(classes) != 0 {
		t.Errorf("本组件不输出控件外观类，得到 %v", classes)
	}
}

// TestFieldCatalogAlignedWithCheckoutChain 字段库必须与结算链路的收参逐条对齐。
//
// 判据是 runtimefragment/cart.go 的 renderCheckout —— 那张 paramOf 调用表就是
// 「结算链路实际收的参数」。清单与它分叉时：多一个 = 凭空发明（没人读它），
// 少一个 = 访客填了会被静默丢弃（页面上看不出任何异常）。
func TestFieldCatalogAlignedWithCheckoutChain(t *testing.T) {
	// 来源：runtimefragment/cart.go 的 renderCheckout（收货 / 账单 / 备注 / 链路参数）。
	want := []string{
		"email", "name", "phone",
		"shipName", "shipPhone", "country", "province", "city", "district", "address", "zip",
		"remark",
		"billName", "billPhone", "billCountry", "billProvince", "billCity", "billDistrict", "billAddress", "billZip",
		"requestId", "locale",
	}
	got := map[string]bool{}
	for _, f := range ordercontract.CheckoutFields() {
		got[f.Key] = true
	}
	for _, key := range want {
		if !got[key] {
			t.Errorf("字段清单缺少结算链路会收的参数 %q", key)
		}
	}
	if len(got) != len(want) {
		t.Errorf("字段清单有 %d 项、结算链路收 %d 项（多出来的必须能说出谁读它）", len(got), len(want))
	}
	// 金额与归属类永远不进字段库（信任边界，见 runtimefragment/cart.go 的运费注释）。
	for _, forbidden := range []string{"shippingTotal", "total", "subtotal", "discountTotal", "userId", "user", "paid"} {
		if got[forbidden] {
			t.Errorf("字段库不该包含 %q（金额与归属是收银台上的东西，不能让客户端填）", forbidden)
		}
	}
}

// groupOf 按分组键查找分组。
func groupOf(groups []GroupView, key string) *GroupView {
	for i := range groups {
		if groups[i].Key == key {
			return &groups[i]
		}
	}
	return nil
}

// contains 字符串切片包含判断（小工具）。
func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
