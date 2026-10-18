package productdto

// product_bulk_resp.go — 批量用例的回执（与 product_bulk_req.go 成对）。
//
// 语义只有一种：**整批 = 一个事务，要么全成、要么全败**。
// 所以这里没有「跳过 / 失败」两个桶，也没有逐条 items 明细 —— 只在整批被拒时给一个错误，
// 错误里带可定位的条目（见 BulkRejectedError）。
//
// 为什么不留「成功 N / 跳过 M」：用户看到那个回执时既不知道哪一条没成，也没法只重试那一条
// （重试会把成功过的再走一遍），而库里已经是他看不懂的中间态 —— 一个表改了一个表没改，
// 这种账没人能对。证据与判据在 internal/module/CLAUDE.md「列表页批量操作」那条。

// BulkResultResp 批量用例的回执：整批完成时给一个计数。
type BulkResultResp struct {
	// Done 完成的项数（= 归一后的请求项数；整批失败时不会走到这里）。
	Done int `json:"done"`
}


// BulkRejectedError 整批拒绝：一句话说结论，明细放 data.items。
//
// 包装而非另造一套：Err 是 facing 的常规业务错误（key + 兜底 + 声明的状态码 409），
// Unwrap 让 pkg/facing.FactOf / StatusOf 沿链读到它们 —— 出口照常走 response.ErrorStatusAuto
// 的同一套判据，不需要认识这个类型。
//
// 控制器要拿到 Items 时 errors.As 一下即可；拿不到时它退化成一个普通业务错误。
type BulkRejectedError struct {
	Err   error
	Items []BulkBlockedItemResp
}

func (e *BulkRejectedError) Error() string { return e.Err.Error() }

func (e *BulkRejectedError) Unwrap() error { return e.Err }

// BulkPricingResultResp 批量改价的回执（在 BulkResultResp 之上多两个改价专属计数）。
type BulkPricingResultResp struct {
	BulkResultResp
	// ChangedVariants 真正改动的变体数（一次改价覆盖商品的多个变体）。
	ChangedVariants int `json:"changedVariants"`
	// UnchangedProducts 本来就在目标价上的商品数（不是失败：service 不写空台账，故单独计数）。
	UnchangedProducts int `json:"unchangedProducts"`
}

// BulkBlockedItemResp 整批被拒时的一条「为什么不行」（data.items 的元素）。
//
// 三个字段是**定位**所需：id（重试时勾哪一条）、name（人读）、为什么。
// 名称优先展示：批量里给人看 uuid 等于没给信息。
//
// ReasonKey 与 Message 是同一件事的两端：service 只回业务 key（它拿不到也不该拿
// 取词函数），出口把 key 翻成当前语言的 Message 再给前端；两者都带 omitempty，
// 谁填了谁出现在 JSON 里。
type BulkBlockedItemResp struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	ReasonKey string `json:"reasonKey,omitempty"`
	Message   string `json:"message,omitempty"`
}
