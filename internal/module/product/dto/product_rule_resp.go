// product_rule_resp.go — 规则参数的**取值**（数字与「有没有」），不含任何文案。
//
// 后台要把「{"days":30}」显示成「上架 30 天内」，但句子本身是前端的事：
// 后端只给 days 的值与「给没给」，模板按 RuleType 分支取词拼句。
// 这样一来同一条规则换措辞只改模板，不需要动 service，也不会出现第二份规则语义。
package productdto

// RuleValuesResp 规则参数的取值集合（各类规则共用一个形状）。
//
// 字段用「值 + Has 布尔」而不是指针：模板里 map 缺键会中断整页，
// struct 的零值则安全 —— Has 为 false 时模板走「参数不合法」分支。
type RuleValuesResp struct {
	// Days 新品规则的「上架 N 天内」。
	Days    int
	HasDays bool
	// Value 定价规则的核心数值（multiplier / amount / margin）。
	Value    float64
	HasValue bool
	// MinPrice / MaxPrice 价格区间规则的两个端点（至少给一个）。
	MinPrice    float64
	HasMinPrice bool
	MaxPrice    float64
	HasMaxPrice bool
}
