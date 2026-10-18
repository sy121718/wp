package productenums

import "net/http"

// product_err_status.go — 商品域业务错误的 **HTTP 语义**（哪些 key 是「不存在」、哪些是「冲突」）。
//
// 为什么状态码住在这里而不是控制器：一个 key 是「找不到」还是「冲突」，只有**声明处**知道。
// 控制器按接口写死状态码时（`Get` 固定传 404、写动作固定传 400），同一个 err 换个接口就换了
// 含义：`Get` 里读库失败的内部错误被报成 404（「商品不存在」），而写动作里「URL 段被占用」
// 被报成 400 —— 前端按状态码做的分支与监控的分类都跟着错，且没有任何地方报错。
//
// 判据只有一份：出口调 response.ErrorStatusAuto，它读 pkg/facing.StatusOf；
// 声明过 404/409 的用它，业务错误默认 400，其余 500。**不要按文案猜**（"NotFound" 出现在
// 字符串里不代表资源不存在），也不要为了「看起来更精确」在出口再包一次 WithStatus ——
// 那就是两份判据，而它们分叉的方向最难查：文案按 A 判、状态码按 B 判。
//
// 只登记**语义确定**的两档：
//   · 404 —— 请求指向的资源不存在（详情 / 删除 / 改价单这类按 id 定位的入口）；
//   · 409 —— 唯一性或状态冲突（占用、被引用、仍有库存、不可更改、跨工程仍被引用）。
// 参数不合法、缺字段、形状不对一律不登记（默认 400）—— 给它们编一个「更具体」的状态码，
// 只会让前端的重试与提示逻辑失去稳定约定。

// errStatuses 业务错误 key → HTTP 状态码。未登记的 key 走 pkg/response 的默认分档
// （业务 400 / 非业务 500）。
var errStatuses = map[string]int{
	// —— 404：资源不存在 ——
	ErrNotFound:                  http.StatusNotFound,
	ErrAttrNotFound:              http.StatusNotFound,
	ErrBrandNotFound:             http.StatusNotFound,
	ErrCategoryNotFound:          http.StatusNotFound,
	ErrTagNotFound:               http.StatusNotFound,
	ErrPricingAdjustmentNotFound: http.StatusNotFound,
	ErrBundleVariantNotFound:     http.StatusNotFound,

	// —— 409：唯一性 / 被引用 / 状态冲突 ——
	ErrBulkRejected:         http.StatusConflict,
	ErrSlugTaken:            http.StatusConflict,
	ErrSkuTaken:             http.StatusConflict,
	ErrContainerSKUTaken:    http.StatusConflict,
	ErrAttrKeyTaken:         http.StatusConflict,
	ErrBrandSlugTaken:       http.StatusConflict,
	ErrCategorySlugTaken:    http.StatusConflict,
	ErrTagSlugTaken:         http.StatusConflict,
	ErrVariantHasStock:      http.StatusConflict,
	ErrAttrInUse:            http.StatusConflict,
	ErrBrandInUse:           http.StatusConflict,
	ErrCategoryInUse:        http.StatusConflict,
	ErrCategoryHasChildren:  http.StatusConflict,
	ErrTagCrossProject:      http.StatusConflict,
	ErrProductTypeImmutable: http.StatusConflict,
	ErrTagNotManual:         http.StatusConflict,
}

// StatusFor 该 key 声明的状态码（未登记返回 0，由出口按默认分档）。
func StatusFor(key string) int { return errStatuses[key] }
