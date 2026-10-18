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

import "go_wp/pkg/bulkids"

// BulkDeleteReq 五个批量删除用例共用的入参。
type BulkDeleteReq struct {
	// IDs 目标 id 清单。入参侧**必须**先过 NormalizeIDs（去空白 / 去重 / 上限 200），
	// service 侧还会再归一一次（service 是唯一的真源，不能依赖调用方守规矩）。
	IDs []string `json:"ids"`
	// ProjectID 工程作用域；为空时由 service 走「唯一工程」兜底。
	// 多工程部署下必须带：空着只剩「唯一工程」兜底一条路，否则 ErrInvalidParam。
	ProjectID string `json:"projectId"`
}

// NormalizeIDs 归一 IDs 并写回（去空白 / 去空 / 去重保序 / 上限 bulkids.MaxIDs）。
//
// 位置在 dto 而不是控制器：五个批量端点 + 页面通道曾经各写一份归一，其中一处漏了上限，
// 超限那批被静默截断而操作者以为「勾的全删了」—— 这是本仓真实发生过的一次故障。
// 归一的判据只能有一处，而且必须是与入参类型绑定的那一处。
//
// 返回的错误可能是 *bulkids.IDsError（超限，带 Max / Count 两个数字），由出口取词成整句。
func (r *BulkDeleteReq) NormalizeIDs() error {
	ids, err := bulkids.Normalize(r.IDs)
	if err != nil {
		return err
	}
	r.IDs = ids
	return nil
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

// NormalizeIDs 归一 ids（判据同 BulkDeleteReq.NormalizeIDs）。
func (r *BulkPricingJSONReq) NormalizeIDs() error {
	ids, err := bulkids.Normalize(r.IDs)
	if err != nil {
		return err
	}
	r.IDs = ids
	return nil
}

// BulkPricingReq 批量改价的用例入参。
type BulkPricingReq struct {
	// IDs 目标商品 id 清单（语义同 BulkDeleteReq.IDs）。
	IDs []string
	// ProjectID 工程作用域；为空时由 service 走「唯一工程」兜底。
	ProjectID string
	// Note 本次调价的备注（写进每一条留痕台账）。
	Note string
	// OperatorID 操作人（inbound 从会话覆盖写入，客户端传入的值被忽略 —— 调价留痕的操作人不可伪造）。
	OperatorID string
	// Rule 定价规则。Scope 与 TargetID 由 service 逐商品覆盖（批量入口恒为
	// 「单个商品的全部变体」，TargetID = 当前商品 id）。
	Rule PricingRuleReq
}
