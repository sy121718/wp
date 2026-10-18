package productdto

// product_bulk_req.go / product_bulk_resp.go — 批量用例的入参（Req）与回执（Resp）。
//
// 文件按「入参 / 返回」成对出现：一个域只有这两份，类型名以 Req / Resp 结尾。
//
// 语义只有一种：**整批 = 一个事务，要么全成、要么全败**（判据见 internal/module/CLAUDE.md
// 「列表页批量操作」）。所以入参里只有 id 清单，回执里没有「跳过 / 失败」两个桶，
// 也没有逐条 items —— 整批被拒时给一个错误，错误里带可定位的条目（见 BulkRejectedError）。
//
// 为什么不留「成功 N / 跳过 M」：用户看到那个回执时既不知道哪一条没成，也没法只重试那一条
// （重试会把成功过的再走一遍），而库里已经是他看不懂的中间态 —— 一个表改了一个表没改，
// 这种账没人能对。

import productenums "go_wp/internal/module/product/enums"

// BulkDeleteReq 五个批量删除用例共用的入参。
type BulkDeleteReq struct {
	// IDs 目标 id 清单，原样交进来即可：归一（去空白 / 去重 / 上限 200）在 service，
	// 入口不做任何预处理 —— 归一判据只有一份，多了就有一处会漏（超限被静默截断）。
	IDs []string `json:"ids"`
	// ProjectID 工程作用域；为空时由 service 走「唯一工程」兜底。
	// 多工程部署下必须带：空着只剩「唯一工程」兜底一条路，否则 ErrInvalidParam。
	ProjectID string `json:"projectId"`
}

// BulkPricingJSONReq 批量改价的 JSON 入参。
//
// 规则字段**平铺在顶层**（不是嵌一个 rule 对象）：基座的批量按钮把表单字段整体序列化，
// 嵌一层就要在前端再拆一次，而拆错的表现是「填了 amount 却按 multiplier 算」。
type BulkPricingJSONReq struct {
	IDs        []string `json:"ids"`
	ProjectID  string   `json:"projectId"`
	Note       string   `json:"note"`
	RuleType   string   `json:"ruleType"`
	Multiplier string   `json:"multiplier"`
	Amount     string   `json:"amount"`
	Margin     string   `json:"margin"`
	Rounding   string   `json:"rounding"`
}

// ServiceReq 转成 service 的入参（形状转换放在 dto：控制器只做「绑定 → 调用 → 响应」，
// 不在这里拼 Rule）。
//
// Scope 与 TargetID 不在这里给：批量入口恒为「单个商品的全部变体」，TargetID 由 service
// 逐商品覆盖 —— 让调用方填等于给一个必须被忽略的字段。
func (r *BulkPricingJSONReq) ServiceReq(operatorID string) *BulkPricingReq {
	return &BulkPricingReq{
		IDs:        r.IDs,
		ProjectID:  r.ProjectID,
		Note:       r.Note,
		OperatorID: operatorID,
		Rule: PricingRuleReq{
			ProjectID:  r.ProjectID,
			RuleType:   r.RuleType,
			Rounding:   r.Rounding,
			Scope:      productenums.PricingScopeProduct,
			RuleParams: PricingParams(r.RuleType, r.Multiplier, r.Amount, r.Margin),
		},
	}
}

// BulkPricingReq 批量改价的用例入参。
type BulkPricingReq struct {
	// IDs 目标商品 id 清单（语义同 BulkDeleteReq.IDs）。
	IDs []string
	// ProjectID 工程作用域；为空时由 service 走「唯一工程」兜底。
	ProjectID string
	// Note 本次调价的备注（写进每一条留痕台账）。
	Note string
	// OperatorID 操作人（入口从会话覆盖写入，客户端传入的值被忽略 —— 调价留痕的操作人不可伪造）。
	OperatorID string
	// Rule 定价规则。Scope 与 TargetID 由 service 逐商品覆盖（批量入口恒为
	// 「单个商品的全部变体」，TargetID = 当前商品 id）。
	Rule PricingRuleReq
}
