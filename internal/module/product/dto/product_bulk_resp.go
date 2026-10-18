package productdto

import "go_wp/pkg/facing"

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
	Items []facing.BlockedItem
}

func (e *BulkRejectedError) Error() string { return e.Err.Error() }

func (e *BulkRejectedError) Unwrap() error { return e.Err }

// BlockedItems 让出口按接口认形状（pkg/response 只认 facing.BlockedError，不认识本类型）。
func (e *BulkRejectedError) BlockedItems() []facing.BlockedItem { return e.Items }

// BulkPricingResultResp 批量改价的回执（在 BulkResultResp 之上多两个改价专属计数）。
type BulkPricingResultResp struct {
	BulkResultResp
	// ChangedVariants 真正改动的变体数（一次改价覆盖商品的多个变体）。
	ChangedVariants int `json:"changedVariants"`
	// UnchangedProducts 本来就在目标价上的商品数（不是失败：service 不写空台账，故单独计数）。
	UnchangedProducts int `json:"unchangedProducts"`
}
