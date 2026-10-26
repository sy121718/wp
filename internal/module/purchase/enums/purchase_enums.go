// Package purchaseenums purchase 模块（货源 / 采购单 / 入库）的响应消息与界面词条 key。
//
// 与 product / inventory 同形：未接 i18n 前 ErrXxx / MsgXxx 是消息键常量，
// handler 与 service 一律引用本包，不硬编码文案。
//
// **key 的字符串值逐一照搬自 inventoryenums**（2026-10 采购独立成模块）：sys_i18n 里的
// 词条按 key 逐字匹配，改一个字符就等于让已发布的译文全部失配、界面回落中文兜底。
// 迁移的行政动作是「搬常量」，不是「重命名 key」。
package purchaseenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

// 响应消息。
const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"

	// —— 采购单与入库 ——
	MsgReceiptSuccess = "MsgReceiptSuccess" // 入库登记成功
)

// 错误消息。
const (
	ErrInvalidParam = "ErrInvalidParam" // 参数错误

	// —— 货源 ——
	ErrSourceNotFound      = "ErrSourceNotFound"      // 货源不存在
	ErrSourceNameRequired  = "ErrSourceNameRequired"  // 货源名称必填
	ErrSourceCodeRequired  = "ErrSourceCodeRequired"  // 货源编码必填
	ErrSourceCodeInvalid   = "ErrSourceCodeInvalid"   // 货源编码只允许大写字母 / 数字 / 下划线，且不超长
	ErrSourceCodeTaken     = "ErrSourceCodeTaken"     // 同工程下货源编码已占用
	ErrSourceTypeInvalid   = "ErrSourceTypeInvalid"   // 类型只认 external / internal（决定是否按内部交易口径出报表）
	ErrSourceStatusInvalid = "ErrSourceStatusInvalid" // 状态不是 active / disabled
	// ErrSourceInternalNotRelated 内部货源必须标记关联方：内部交易必须能被关联方报表捕获。
	ErrSourceInternalNotRelated = "ErrSourceInternalNotRelated"
	// ErrSourceSettleNotInternal 结算价只属于内部货源（自产商品的成本口径，外部供应商走采购价）。
	ErrSourceSettleNotInternal = "ErrSourceSettleNotInternal"
	ErrSourceSettleInvalid     = "ErrSourceSettleInvalid" // 结算价不能为负
	ErrSourceConfigInvalid     = "ErrSourceConfigInvalid" // 对接配置必须是 JSON 对象
	ErrSourceFilterInvalid     = "ErrSourceFilterInvalid" // 报表筛选参数不合法（关联方标志只认 true / false）

	// —— 采购单 ——
	ErrPurchaseOrderNotFound   = "ErrPurchaseOrderNotFound"   // 采购单不存在
	ErrPurchaseCodeRequired    = "ErrPurchaseCodeRequired"    // 采购单号必填
	ErrPurchaseCodeInvalid     = "ErrPurchaseCodeInvalid"     // 采购单号只允许大写字母 / 数字 / 下划线 / 连字符，且不超长
	ErrPurchaseCodeTaken       = "ErrPurchaseCodeTaken"       // 同工程下采购单号已占用
	ErrPurchaseSourceRequired  = "ErrPurchaseSourceRequired"  // 采购单必须指定货源
	ErrPurchaseSourceDisabled  = "ErrPurchaseSourceDisabled"  // 已停用的货源不能下采购单（停用 = 不再选用）
	ErrPurchaseLinesRequired   = "ErrPurchaseLinesRequired"   // 采购单至少要有一行
	ErrPurchaseLinesTooMany    = "ErrPurchaseLinesTooMany"    // 采购行数超过上限
	ErrPurchaseLineRequired    = "ErrPurchaseLineRequired"    // 采购行必须给出变体
	ErrPurchaseLineDuplicate   = "ErrPurchaseLineDuplicate"   // 同一采购单里同一个 SKU 重复出现
	ErrPurchaseQuantityInvalid = "ErrPurchaseQuantityInvalid" // 采购数量必须为正整数
	ErrPurchasePriceInvalid    = "ErrPurchasePriceInvalid"    // 采购单价必须为正数
	ErrPurchaseStatusInvalid   = "ErrPurchaseStatusInvalid"   // 状态筛选值不在五个合法状态内（迁移 626 起多了 finished / cancelled）
	ErrPurchaseLinesLocked     = "ErrPurchaseLinesLocked"     // 已有入库数量的采购单不能再改行
	ErrPurchaseLineNotFound    = "ErrPurchaseLineNotFound"    // 采购行不存在（或不属于该采购单）

	// —— 入库 ——
	ErrReceiptLinesRequired   = "ErrReceiptLinesRequired"   // 入库清单不能为空
	ErrReceiptLineDuplicate   = "ErrReceiptLineDuplicate"   // 同一入库请求里同一采购行只能出现一次
	ErrReceiptQuantityInvalid = "ErrReceiptQuantityInvalid" // 入库数量必须为正整数
	// ErrReceiptOverReceive 入库数量超过「采购数量 - 已入库数量」：超收在数据层即不可能，
	// 服务层在同一事务内用带守卫的原子递增拒绝（不留半截、不靠读-改-写）。
	ErrReceiptOverReceive    = "ErrReceiptOverReceive"
	ErrReceiptOrderDone      = "ErrReceiptOrderDone"      // 采购单已全部入库，无需再收
	ErrReceiptRequestInvalid = "ErrReceiptRequestInvalid" // 幂等键不合法（超长 / 非法字符）
	// ErrProductionSourceNotInternal 生产入库只认内部货源（自家工厂）：外部供应商走采购单。
	ErrProductionSourceNotInternal = "ErrProductionSourceNotInternal"
	ErrProductionVariantRequired   = "ErrProductionVariantRequired" // 生产入库必须给出 SKU 变体
	ErrProductionCostInvalid       = "ErrProductionCostInvalid"     // 生产入库的成本价必须手工填写且非负
	// ErrVariantCostPortMissing 商品侧成本价端口未注入（装配缺陷 / 纯采购单测路径）。
	ErrVariantCostPortMissing = "ErrVariantCostPortMissing"

	// —— 采购单终态（迁移 626，W2）——
	//
	// 前三个中间态（pending / partial / received）由「已入库数量 与 采购数量」推导，
	// 没有任何人工置位入口。两个终态是**人工决定**：
	//   · finished  —— 提前关单：剩下的不再等；
	//   · cancelled —— 取消：只允许在还没收满时（收满的货已经进仓，要走退货）。
	ErrPurchaseStatusNotFinishable = "ErrPurchaseStatusNotFinishable"
	ErrPurchaseStatusNotCancelable = "ErrPurchaseStatusNotCancelable"

	// —— 入库接收的仓库侧 SKU 编码归一 ——
	//
	// 编码归一后仍为空（给了空串 / 只给了「SZ_」）时拒绝：ERP 侧同一条口径
	// （inventory 的 ErrStockSKURequired 是同一个 key，必须保留同一句中文）。
	ErrStockSKURequired = "ErrStockSKURequired"
)

// 货源类型取值（写入即校验，不接受自由文本）。
//
//	external —— 外部供应商（第三方，采购走采购单）；
//	internal —— 集团内（关联公司 / 自家工厂，可设内部结算价，自家工厂走生产入库）。
//
// 类型不是装饰性标签：它是「这笔交易是否按内部交易口径出报表」的判据，
// 内部货源恒为关联方（见 Service.CreateSource）。
const (
	SourceTypeExternal = "external"
	SourceTypeInternal = "internal"
)

// 货源状态取值（写入即校验，不接受自由文本）。
const (
	SourceStatusActive   = "active"
	SourceStatusDisabled = "disabled"
)

// 通用启停取值（与货源状态同值，语义是「这一行还在不在用」）。
//
// 保留两份名字而不是让调用方混用：SourceStatus* 是**货源列的取值域**，
// Status* 是展示层判「启用中 / 已停用」用的通用档 —— 将来某个非货源对象
// 也需要启停文案时，不必借用货源的名字。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// 采购单状态取值。
//
// 三种状态**全部由「已入库数量 与 采购数量」推导**（没有任何人工置位入口）：
//
//	pending  —— 所有行的已入库数量都是 0（未入库）；
//	partial  —— 有入库但至少一行还没收满（部分入库）；
//	received —— 每一行的已入库数量都等于采购数量（已入库）。
//
// 推导在登记入库的同一事务内完成并写回，因此状态永远与行数据自洽。
const (
	PurchaseStatusPending  = "pending"
	PurchaseStatusPartial  = "partial"
	PurchaseStatusReceived = "received"
	// PurchaseStatusFinished 提前关单（迁移 626）：人工决定「剩下的不再等」。
	PurchaseStatusFinished = "finished"
	// PurchaseStatusCancelled 取消（迁移 626）：只允许在还没收满时。
	PurchaseStatusCancelled = "cancelled"
)

// 入库单类型。
//
//	purchase   —— 采购收货：必有采购单，来源是货源，成本价取采购单价；
//	production —— 自家工厂生产入库：无采购单，成本价手工填写。
const (
	ReceiptKindPurchase   = "purchase"
	ReceiptKindProduction = "production"
)

// 入库单状态（入库是「先记账再动库存」）。
//
//	pending —— 记账已落库、库存变动尚未完成（中间态；进程中断时会停在这里，后台可见）；
//	posted  —— 库存变动已提交并记下批次号（正常终态）。
//
// 变动失败不留 failed 单据：事务整体回滚、单据一起回滚，状态列只有这两态。
const (
	ReceiptStatusPending = "pending"
	ReceiptStatusPosted  = "posted"
)

// 库存变动方向（与库存流水的方向协议同一组取值）。
//
// 采购侧只用得到 in：入库就是「方向 in 的一次变动」，出库与盘点在库存模块自己的页面里。
// 值属于库存流水的对外协议，因此改值等于改协议。
const (
	DirectionIn = "in"
)

// ErrStockLinesTooMany 单次动变的行数超过单批上限（key 与库存款逐字一致，措辞「变动清单行数超过单批上限」）。
const ErrStockLinesTooMany = "ErrStockLinesTooMany"

// 入库产生的库存流水来源引用（source_type 列）。
//
// 采购收货的 source_ref 是采购单号，生产入库的 source_ref 是入库单号 ——
// 流水可以反向定位到是哪张单据动的库存。**值属于库存流水的对外协议**
// （inventory 的流水读模型按它渲染来源列），因此改值等于改协议。
const (
	MovementSourcePurchaseOrder = "purchase_order"
	MovementSourceProduction    = "production"
)

// 入库使用的**内置**变动原因 code（inventory 的原因字典表里已 seed）。
const (
	ReasonCodePurchaseIn   = "purchase_in"
	ReasonCodeProductionIn = "production_in"
)

// 后台页面标题（i18n key）。
//
// 值就是 i18n key：adminui.Prepare 会做 t(title, title)，词条命中则显示译文、
// 未命中回退字面量。因此**新增常量必须同批 seed 词条**（迁移 217），
// 否则顶栏会把 key 原样显示出来。
const (
	// MsgInventorySourcesTitle 货源管理页标题。
	MsgInventorySourcesTitle = "MsgInventorySourcesTitle"
	// MsgInventoryPurchasesTitle 采购入库页标题。同上，词条见迁移 217。
	MsgInventoryPurchasesTitle = "MsgInventoryPurchasesTitle"
)

// errFallbacks 每个错误常量的中文兜底（与声明的 key 一起交给 pkg/facing.New）。
//
// 兜底是**声明处**的话：词条缺失 / i18n 未初始化时，出口至少能给出这句中文，而不是把
// ErrReceiptOrderDone 这种标识符摆给运营看。措辞逐一照搬自 inventoryenums ——
// 同一个 key 在两处给不同兜底，会让「采购页」与「库存页」对同一件事说两句话。
var errFallbacks = map[string]string{
	ErrInvalidParam:                "参数错误",
	ErrSourceNotFound:              "货源不存在",
	ErrSourceNameRequired:          "货源名称必填",
	ErrSourceCodeRequired:          "货源编码必填",
	ErrSourceCodeInvalid:           "货源编码只允许大写字母 / 数字 / 下划线，且不超长",
	ErrSourceCodeTaken:             "同工程下货源编码已占用",
	ErrSourceTypeInvalid:           "类型只认 external / internal（决定是否按内部交易口径出报表）",
	ErrSourceStatusInvalid:         "状态不是 active / disabled",
	ErrSourceInternalNotRelated:    "内部货源必须标记关联方：内部交易必须能被关联方报表捕获。",
	ErrSourceSettleNotInternal:     "结算价只属于内部货源（自产商品的成本口径，外部供应商走采购价）。",
	ErrSourceSettleInvalid:         "结算价不能为负",
	ErrSourceConfigInvalid:         "对接配置必须是 JSON 对象",
	ErrSourceFilterInvalid:         "报表筛选参数不合法（关联方标志只认 true / false）",
	ErrPurchaseOrderNotFound:       "采购单不存在",
	ErrPurchaseCodeRequired:        "采购单号必填",
	ErrPurchaseCodeInvalid:         "采购单号只允许大写字母 / 数字 / 下划线 / 连字符，且不超长",
	ErrPurchaseCodeTaken:           "同工程下采购单号已占用",
	ErrPurchaseSourceRequired:      "采购单必须指定货源（来源 = 货源管理的货源）",
	ErrPurchaseSourceDisabled:      "已停用的货源不能下采购单（停用 = 不再选用）",
	ErrPurchaseLinesRequired:       "采购单至少要有一行",
	ErrPurchaseLinesTooMany:        "采购行数超过上限",
	ErrPurchaseLineRequired:        "采购行必须给出变体",
	ErrPurchaseLineDuplicate:       "同一采购单里同一个 SKU 重复出现",
	ErrPurchaseQuantityInvalid:     "采购数量必须为正整数",
	ErrPurchasePriceInvalid:        "采购单价必须为正数",
	ErrPurchaseStatusInvalid:       "状态筛选值不在合法状态内（未入库 / 部分入库 / 已入库 / 已关单 / 已取消）",
	ErrPurchaseLinesLocked:         "已有入库数量的采购单不能再改行（改了状态推导就不成立）",
	ErrPurchaseLineNotFound:        "采购行不存在（或不属于该采购单）",
	ErrPurchaseStatusNotFinishable: "只有已入库或部分入库的采购单可以提前关单",
	ErrPurchaseStatusNotCancelable: "只有未入库或部分入库的采购单可以取消（已收满的货已进仓，要走退货）",
	ErrReceiptLinesRequired:        "入库清单不能为空",
	ErrReceiptLineDuplicate:        "同一入库请求里同一采购行只能出现一次",
	ErrReceiptQuantityInvalid:      "入库数量必须为正整数",
	ErrReceiptOverReceive:          "入库数量超过「采购数量 - 已入库数量」：超收在数据层即不可能。",
	ErrReceiptOrderDone:            "采购单已全部入库，无需再收",
	ErrReceiptRequestInvalid:       "幂等键不合法（超长 / 非法字符）",
	ErrProductionSourceNotInternal: "生产入库只认内部货源（自家工厂）：外部供应商走采购单。",
	ErrProductionVariantRequired:   "生产入库必须给出 SKU 变体",
	ErrProductionCostInvalid:       "生产入库的成本价必须手工填写且非负",
	ErrVariantCostPortMissing:      "商品侧成本价端口未注入（装配缺陷 / 纯采购单测路径）。",
	ErrStockSKURequired:            "缺少仓库侧 SKU 编码：请传裸码（不带仓码前缀，如 DRAWERSMOKE_001）",
	ErrStockLinesTooMany:           "变动清单行数超过单批上限",
}

// errStatuses 业务错误对外的 HTTP 状态码（**只登记非 400 的档**）。
//
// 400 是业务错误的默认档（参数不合法、业务拒绝），所以这里只有两类：
//
//	404 —— 找不到：重试无用，调用方该换 id / 刷新列表；
//	409 —— 冲突：唯一键占用、状态不允许、超收 —— 处置是「刷新后重试」或换个值。
var errStatuses = map[string]int{
	// —— 404 找不到 ——
	ErrSourceNotFound:        http.StatusNotFound,
	ErrPurchaseOrderNotFound: http.StatusNotFound,
	ErrPurchaseLineNotFound:  http.StatusNotFound,

	// —— 409 冲突 / 当前状态不允许 ——
	ErrSourceCodeTaken:             http.StatusConflict,
	ErrPurchaseCodeTaken:           http.StatusConflict,
	ErrPurchaseSourceDisabled:      http.StatusConflict,
	ErrPurchaseLinesLocked:         http.StatusConflict,
	ErrPurchaseStatusNotFinishable: http.StatusConflict,
	ErrPurchaseStatusNotCancelable: http.StatusConflict,
	ErrReceiptOverReceive:          http.StatusConflict,
	ErrReceiptOrderDone:            http.StatusConflict,
}

// Err 声明一条自带对外文案的业务错误（pkg/facing.New：key + 中文兜底）。
func Err(key string) error {
	return facing.WithStatus(facing.New(key, errFallbacks[key]), errStatuses[key])
}

// ErrDetail 声明一条带明细的业务错误：明细拼在半角冒号之后。
//
// **必须半角**：pkg/facing.FactOf 只按 ": " 切明细段（页面出口再按 i18n.ParseErrorDetails
// 解析明细词条），写全角「：」会让整条过不了判据，退化成内部错误。
func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

// FallbackFor 取一个 key 的中文兜底（调用方只有 key、没有事实时用）。
func FallbackFor(key string) string {
	return errFallbacks[key]
}
