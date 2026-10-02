package ordercontract

// data_source.go — 订单模块对外暴露的**结算表单字段库**（构建期数据源，issue #35）。
//
// 为什么字段库在订单契约里，而不是在组件 Props 里：
//
//	结算表单的字段不是「作者随便加的输入框」——每一个都会成为 orders / order_addresses
//	的一次写入。字段库因此必须与**订单契约**同源：
//	  · 落库约束：字段必须有列承接（orders 表没有 country 列时，表单收 country 就是
//	    一个必然失败的提交）；
//	  · 同源约束：静态产物里烘了表单，片段端按字段清单收参。清单与产物分叉时，
//	    访客填了字段会被片段静默丢弃 —— 页面上看不出任何异常。
//
// 权威来源：internal/module/runtimefragment/cart.go 的 renderCheckout —— 逐个
// paramOf 调用就是「结算链路实际收的参数」。清单与那张表**必须逐条对齐**，
// 谁多一个都是缺陷：多的是凭空发明的字段（没人读它），少的是访客填了被丢掉的字段。
//
// 越权边界（与 ProductDataSource 同一判据，见 product/contract/data_source.go）：
// 本文件只有**读**能力，写方法一个都不在 —— 能改字段库就等于能让访客提交一个
// 订单表没有的列。国家码这类「只影响地址一行文字」的字段在此列出；运费 / 金额 /
// 归属（userId）**永远不进字段库**：它们是收银台上的钱与身份，见 cart.go 的
// 「运费刻意不从表单取」那段。

// CheckoutFieldType 字段的输入形态（构建期据此选控件：文本 / 下拉 / 多行）。
//
// 它**不是**校验规则：必填与长度由服务端在收参时判（cart 模块），
// 这里只回答「长什么样」，避免在组件层出现第二份业务校验。
type CheckoutFieldType string

const (
	// CheckoutFieldText 单行文本。
	CheckoutFieldText CheckoutFieldType = "text"
	// CheckoutFieldEmail 邮箱（浏览器原生 type=email 键盘与格式提示）。
	CheckoutFieldEmail CheckoutFieldType = "email"
	// CheckoutFieldTel 电话（触屏弹数字键盘）。
	CheckoutFieldTel CheckoutFieldType = "tel"
	// CheckoutFieldTextarea 多行文本（备注）。
	CheckoutFieldTextarea CheckoutFieldType = "textarea"
	// CheckoutFieldCountry 国家/地区：**原生 select**，选项由构建期烘焙
	// （core.checkoutForm 从 RenderContext 取，见 builder/core/render.go 的 CheckoutInput）。
	CheckoutFieldCountry CheckoutFieldType = "country"
)

// 展示分组：组件按它分块渲染（联系信息 / 收货地址 / 账单地址），
// 分组不写在组件 Props 里 —— 那样每加一个字段就要在编辑器里手工分组一次。
const (
	// CheckoutGroupContact 联系信息（邮箱 / 姓名 / 电话）。
	CheckoutGroupContact = "contact"
	// CheckoutGroupShipping 收货地址（含订单备注）。
	CheckoutGroupShipping = "shipping"
	// CheckoutGroupBilling 账单地址（bill* 一组）。
	CheckoutGroupBilling = "billing"
	// CheckoutGroupSystem 链路参数（幂等键 / 语言）：参与收参校验，但**不进表单**。
	CheckoutGroupSystem = "system"
)

// CheckoutField 结算表单字段库的一项。
type CheckoutField struct {
	// Key 提交参数名（与 cart.go 的 paramOf key 逐字一致）。
	Key string
	// Label 中文默认标签（作者可在组件 Props 里覆盖；作者填了就以作者的为准）。
	Label string
	// Type 输入形态。
	Type CheckoutFieldType
	// Group 展示分组（CheckoutGroup* 常量）。
	Group string
	// Required 清单层面必填。
	//
	// 判据是**服务端真的会拒**：email / name / phone / 收货详细地址在
	// cart.Checkout 里各有一条显式校验（见 cart/service/cart_checkout.go）。
	// 必填字段不允许从表单移除 —— 关掉它等于让每一次提交必然被拒。
	Required bool
	// Removable 是否允许站长从表单里移除。
	//
	// 与 Required 的分工：Required 说「服务端要不要求」，Removable 说「站长能不能不收集」。
	// 非必填字段关掉是正当的（非中国站点不收集省 / 市 / 区，或只发到本国不收集国家）。
	Removable bool
	// DefaultOn 插入组件时默认是否勾选。
	//
	// 收货人 / 收货电话默认**不勾**：cart.go 里它们的缺省就是联系人姓名与电话
	//（firstNonEmpty(shipName, name)），多问一遍等于把「寄给别人」当成常态。
	DefaultOn bool
}

// InForm 本字段是否允许出现在结算表单里（system 组不进表单）。
func (f CheckoutField) InForm() bool { return f.Group != CheckoutGroupSystem }

// checkoutFields 字段库（本文件是它的唯一声明处）。
//
// 顺序即默认渲染顺序：联系信息 → 收货地址 → 账单地址。账单地址的每一项与收货项
// 一一对应（bill 前缀），这样「是否收集账单地址」打开时，两块的字段顺序天然一致。
var checkoutFields = []CheckoutField{
	// --- 联系信息（cart.go 的 email / name / phone）---
	{Key: "email", Label: "邮箱", Type: CheckoutFieldEmail, Group: CheckoutGroupContact, Required: true, DefaultOn: true},
	{Key: "name", Label: "姓名", Type: CheckoutFieldText, Group: CheckoutGroupContact, Required: true, DefaultOn: true},
	{Key: "phone", Label: "电话", Type: CheckoutFieldTel, Group: CheckoutGroupContact, Required: true, DefaultOn: true},

	// --- 收货地址 ---
	// 收货人 / 收货电话：cart.go 收 shipName / shipPhone，但缺省沿用联系人，
	// 因此默认不勾（勾了反而要求访客重复填写）。
	{Key: "shipName", Label: "收货人", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true},
	{Key: "shipPhone", Label: "收货电话", Type: CheckoutFieldTel, Group: CheckoutGroupShipping, Removable: true},
	// 国家：key 是 country（账单侧是 billCountry），与 cart.go 的 orderCountryCode(paramOf(...)) 一致。
	// 非必填：OrderAddress.Country 空 = 未收集（非中国站点），且它只是地址的一行文字，不影响金额。
	{Key: "country", Label: "国家/地区", Type: CheckoutFieldCountry, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	{Key: "province", Label: "省/州", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	{Key: "city", Label: "城市", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	{Key: "district", Label: "区/县", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	// 详细地址必填：cart.Checkout 里 `shipping.Address == ""` 直接拒单。
	{Key: "address", Label: "详细地址", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Required: true, DefaultOn: true},
	{Key: "zip", Label: "邮编", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	{Key: "remark", Label: "订单备注", Type: CheckoutFieldTextarea, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},

	// --- 账单地址（bill* 一组，cart.go 逐个 paramOf）---
	{Key: "billName", Label: "账单姓名", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billPhone", Label: "账单电话", Type: CheckoutFieldTel, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billCountry", Label: "账单国家/地区", Type: CheckoutFieldCountry, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billProvince", Label: "账单省/州", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billCity", Label: "账单城市", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billDistrict", Label: "账单区/县", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billAddress", Label: "账单详细地址", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billZip", Label: "账单邮编", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},

	// --- 链路参数（不进表单）---
	// requestId 是幂等键：它**必须由发起方在提交那一刻生成**，静态产物对所有人是同一份字节，
	// 烘一个常量进去等于让全站共用一个幂等键（第二单会被当成重放）。
	{Key: "requestId", Label: "请求标识", Type: CheckoutFieldText, Group: CheckoutGroupSystem},
	// locale 决定开号邮件用哪套模板（orderdto.CreateOrderReq.Locale）。它由构建期烘焙
	//（当前产物是哪一份语言，就是哪一份），不让访客看见、也不让访客改。
	{Key: "locale", Label: "语言", Type: CheckoutFieldText, Group: CheckoutGroupSystem},
}

// CheckoutFields 返回结算表单字段库（副本，调用方改不动声明表）。
func CheckoutFields() []CheckoutField {
	out := make([]CheckoutField, len(checkoutFields))
	copy(out, checkoutFields)
	return out
}

// CheckoutFormFields 返回**可以出现在表单里**的字段（system 组之外的全部）。
func CheckoutFormFields() []CheckoutField {
	out := make([]CheckoutField, 0, len(checkoutFields))
	for _, f := range checkoutFields {
		if f.InForm() {
			out = append(out, f)
		}
	}
	return out
}

// CheckoutFieldOf 按 key 取字段；第二个返回值为 false 表示「清单里没有这个 key」。
//
// 调用方（构建期组件校验）据此**报错而不是静默跳过**：静默跳过会让作者在编辑器里
// 配了一个永远不会生效的字段，而页面上没有任何异常。
func CheckoutFieldOf(key string) (CheckoutField, bool) {
	for _, f := range checkoutFields {
		if f.Key == key {
			return f, true
		}
	}
	return CheckoutField{}, false
}

// CheckoutFieldSource 结算表单字段库的**受限读取接口**（issue #35 的构建期数据源形状）。
//
// 形状参照 product/contract/data_source.go：只有读、只有一件事。差别在数据来源 ——
// 商品数据源后面是数据库（集合 / 维度 / 可筛值），字段库后面是**代码里的静态表**，
// 因此这里只有「有哪些字段」一个方法，没有集合解析与元数据那一套。
//
// 实现由契约包自带（StaticCheckoutFieldSource）：静态表不需要服务实例承载。
// 保留接口是为了让消费侧（构建期组件 / 将来的片段端校验）依赖**形状**而不是
// 具体变量 —— 也为了让「谁可以读字段库」有一条可断言的边界。
type CheckoutFieldSource interface {
	// CheckoutFields 返回结算表单字段库（顺序即默认渲染顺序）。
	CheckoutFields() []CheckoutField
}

// StaticCheckoutFieldSource 字段库的静态实现。
type StaticCheckoutFieldSource struct{}

// CheckoutFields 实现 CheckoutFieldSource。
func (StaticCheckoutFieldSource) CheckoutFields() []CheckoutField { return CheckoutFields() }

var _ CheckoutFieldSource = StaticCheckoutFieldSource{}
