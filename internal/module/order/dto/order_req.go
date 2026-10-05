package orderdto

import "strings"

// order_req.go — 订单模块请求（BIZ-1 销售侧）。
//
// 注意这里**没有价格字段**：订单项的单价由服务端从商品模块读取并落快照，
// 绝不接受调用方传入。客户端能传价格的接口等于把收银台交给客人自己看。
//
// 操作人（operatorId / operatorName）与来源（ipAddress / userAgent / userId）一律
// 由 inbound 覆盖写入：这些字段用 json:"-" 屏蔽，客户端传了也不生效。

// NormalizeCountryCode 国家/地区代码的**形状约束**：恰好两个 ASCII 字母才收，
// 通过后大写归一化，其余一律丢弃成空串。
//
// 放在 OrderAddress 同包并导出：访客结算片段与后台代客建单页都要做同一件事，
// 两份私有副本一定会漂移 —— 而漂移的表现是「两条入口落库的国家码形态不一致」
// （一条干净、另一条带着 "China" 这种串），只有写库那一刻才炸。
//
// 为什么只约束形状、不做「这个国家存不存在」的校验：
//   - 表单与接口入参都是客户端可控输入，而国家码**不影响金额与库存** —— 与结算请求里
//     那句「运费刻意不从表单取」（收银台上的钱不能让客人自己填）是同一节的两种情形：
//     能左右金额的字段一个都不收，只是地址一行文字的字段只约束形状；
//     把 country 填成 "ZZ" 不会让订单少收一分钱，它只是收件地址里的一行。
//   - 真正的判据是**长度**：库里是 VARCHAR(2)（迁移 501），放进去超过两个字符的串会让
//     整单落库失败，而调用方看到的只会是一句「下单失败」—— 三个字母的 "CHN"、国家全名
//     "China"、乃至中文名都在这里被挡掉，而不是拖到写库那一刻才炸。
//   - 「码有没有对应国家」属展示层的事：查不到就显示代码本身（展示标签，不是订单语义），
//     把字典查询拉进收参路径只会让建单多一次查库、并在字典缺行时拒绝一笔合法订单。
//
// 大写归一化是必须的：sys_area 的国家 code 是 ISO 3166-1 alpha-2 的大写口径（CN / US），
// 大小写混着存会让「按码取名」的展示静默查不到（页面显示 cn 而不是「中国」）。
func NormalizeCountryCode(raw string) string {
	v := strings.TrimSpace(raw)
	if len(v) != 2 {
		return ""
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return ""
		}
	}
	return strings.ToUpper(v)
}

// OrderItemReq 下单商品项：只传变体 id 与数量（价格与名称由服务端填）。
type OrderItemReq struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
}

// OrderAddress 地址（收货与账单共用同一形状）。
type OrderAddress struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
	Address  string `json:"address"`
	Zip      string `json:"zip"`
	// Country 国家/地区代码（ISO 3166-1 alpha-2，如 CN）。空 = 未收集（非中国站点 / 存量数据）。
	//
	// 存的是**下单时刻的代码快照**，与地址其余几段同性质：展示时按当前语言查字典取名
	// 只是标签，改字典不改历史订单。
	Country string `json:"country"`
}

// CreateOrderReq 建单请求。
type CreateOrderReq struct {
	ProjectID          string         `json:"projectId"`
	CustomerEmail      string         `json:"customerEmail"`
	CustomerName       string         `json:"customerName"`
	CustomerPhone      string         `json:"customerPhone"`
	Items              []OrderItemReq `json:"items"`
	Shipping           OrderAddress   `json:"shipping"`
	Billing            OrderAddress   `json:"billing"`
	PaymentMethod      string         `json:"paymentMethod"`
	PaymentMethodTitle string         `json:"paymentMethodTitle"`
	// ShippingTotal 由调用方给出（运费策略不属于商品域）。
	// DiscountTotal 无 CouponCode 时一律忽略；有券时以服务端试算为准。
	// Subtotal / Total 由服务端按商品价格算出，不接受传入。
	ShippingTotal int64 `json:"shippingTotal"`
	DiscountTotal int64 `json:"discountTotal"`
	// CouponCode 优惠码。给了它就以**服务端试算**的折扣为准并忽略 DiscountTotal ——
	// 客户端能定价的接口等于把收银台交给客人自己看。
	// 核销与建单在同一个事务里完成，不会出现「单建了、券没核销」或反之。
	CouponCode string `json:"couponCode"`
	Remark     string `json:"remark"`
	// RequestID 幂等键：同一键重复提交只落一单。
	RequestID string `json:"requestId"`
	// AdminNote 后台备注：后台代客订单与代发订单的填写位置。
	AdminNote string `json:"adminNote"`
	// Locale 访客语言：决定自动开号的初始密码邮件用哪套模板（空 = 通用模板）。
	Locale string `json:"locale"`
	// ProvisionGuestAccount 是否给这个邮箱开号并把初始密码寄过去（**显式请求字段**）。
	//
	// 三态，且是**唯一**的开号依据；订单来源由建单入口决定，不参与开户判定
	// （见 docs/02-W-admin-order-create.md §4）。
	//
	//	nil   —— 调用方未表态。只有既有的前台 checkout 链路会这样：它走购物车结算，
	//	         没有「是否开户」这个决定，因此保持既有行为（下单即开户），前台行为逐字不变；
	//	false —— 明确不开号。后台代客建单页的**默认档**（复选框不勾选就提交 false）：
	//	         订单照常落库，user_id 留空，不发任何邮件；
	//	true  —— 明确开号：邮箱没有账号就建号并发初始密码（邮箱已有账号时只关联、绝不改密码）。
	//
	// 为什么用 *bool 而不是 bool：bool 的零值 false 无法区分「明确不开号」与「老调用方没传」，
	// 一旦把零值当「不开号」，前台的「下单即开户」会在一夜之间静默消失（既有两条开号用例即判据）。
	ProvisionGuestAccount *bool `json:"provisionGuestAccount"`
	// Attribution 归因与轨迹（流量来源 / 广告参数 / 会话 / 下单前浏览轨迹）。
	// 由 inbound 从访客追踪上下文组装，允许为 nil（后台代客下单没有访客上下文）。
	Attribution *Attribution `json:"attribution"`

	// 以下由 inbound 覆盖写入，客户端不可伪造。
	UserID    *uint64 `json:"-"`
	IPAddress string  `json:"-"`
	UserAgent string  `json:"-"`
	CreateBy  uint64  `json:"-"`
}

// ListOrderReq 订单列表查询。
type ListOrderReq struct {
	ProjectID     string  `form:"projectId"`
	Status        string  `form:"status"`
	Keyword       string  `form:"keyword"`
	PaymentMethod string  `form:"paymentMethod"`
	Offset        int     `form:"offset"`
	Limit         int     `form:"limit"`
	UserID        *uint64 `form:"-"`
}

// GetOrderReq 订单详情查询。
type GetOrderReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	OrderID   uint64 `form:"orderId" json:"orderId"`
}

// ChangeStatusReq 状态流转。
type ChangeStatusReq struct {
	OrderID  uint64 `json:"orderId"`
	ToStatus string `json:"toStatus"`
	Remark   string `json:"remark"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

// CancelOrderReq 取消订单（会归还库存）。
type CancelOrderReq struct {
	OrderID uint64 `json:"orderId"`
	Reason  string `json:"reason"`
	// ProjectID 订单所属工程（DB-009 第三批）：orders 带 FORCE 策略，取消是一次
	// 「加锁读 + 更新 + 写状态日志」的事务，不给作用域时三步全部静默落空。
	// 超时取消扫描已逐工程传入；后台手工取消的调用点（dashboard）目前不带 ——
	// 那需要它把页面上的工程一起传下来（见 DB-009 剩余清单）。
	ProjectID string `json:"-"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

// PayOrderReq 支付落账：网关扣款成功后把订单推进到「已付款」。
//
// 只接受内部订单 id，不支持按商户单号找单：当前唯一的调用方是结算流程，
// 它刚建完单、手上就是 id。将来接真网关的异步回调时再补「按单号加锁」的入口 ——
// 现在写出来没有消费方，只会是一段没人跑的代码。
type PayOrderReq struct {
	OrderID uint64 `json:"orderId"`

	// 支付通道信息落的是订单列（对账要看），所以由调用方给出而不是订单域猜。
	PaymentMethod      string `json:"paymentMethod"`
	PaymentMethodTitle string `json:"paymentMethodTitle"`
	TransactionID      string `json:"transactionId"`
	Remark             string `json:"remark"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

// RefundOrderReq 退款。
type RefundOrderReq struct {
	OrderID       uint64 `json:"orderId"`
	Reason        string `json:"reason"`
	TransactionID string `json:"transactionId"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

// UpdateOrderNoteReq 改订单的后台备注（adminNote）。
//
// 只改这一列：备注**不是状态流转**（它不改变订单处在哪一步），所以不写 status_logs ——
// 把备注变更混进流转链，会让「这单什么时候发的货」变得要翻记录才能看出来。
// 客户填的 remark 不在这里（那是客户的话，后台不该替它改写）。
type UpdateOrderNoteReq struct {
	OrderID   uint64 `json:"orderId" form:"orderId"`
	AdminNote string `json:"adminNote" form:"adminNote"`

	// 操作人由 inbound 覆盖写入，客户端不可伪造。
	OperatorType string `json:"-" form:"-"`
	OperatorID   uint64 `json:"-" form:"-"`
	OperatorName string `json:"-" form:"-"`
}

// GetOrderByNoReq 按商户单号取订单。
//
// 支付通道的异步回调只带商户单号（它不认识我们的自增 id），而 PayOrder 只接受内部 id ——
// 这条入口就是两者之间的那层翻译。
type GetOrderByNoReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	OrderNo   string `form:"orderNo" json:"orderNo"`
}

// VisitorOrderListReq 访客查自己的订单。
//
// UserID 由片段层从访客会话写入；归属过滤落在 SQL 条件里（user_id = ?），
// 而不是「查出来再比对」—— 后者一旦有人调换了查询顺序，越权就只剩一行代码的距离。
type VisitorOrderListReq struct {
	ProjectID string
	Status    string
	Offset    int
	Limit     int
	UserID    uint64
}

// VisitorOrderDetailReq 访客查自己的订单详情（同样按 user_id 收口）。
type VisitorOrderDetailReq struct {
	OrderID   uint64
	ProjectID string
	UserID    uint64
}

// CouponSaveReq 优惠码新建 / 修改。
//
// 时间窗用**字符串**而不是 *time.Time：同一份结构既要服务后台原生表单（form）
// 也要服务 JSON 接口，而表单里的日期天生是文本。解析只发生在 service，
// 非法格式在那里统一报错，而不是让两种绑定器各解出一套结果。
type CouponSaveReq struct {
	ID        uint64 `form:"id" json:"id"`
	ProjectID string `form:"projectId" json:"projectId"`
	// Code 券码：建后不可改 —— 改码等于换一张券，历史核销记录会指向一个查不到的码。
	Code string `form:"code" json:"code"`
	Name string `form:"name" json:"name"`
	// DiscountType percent（折扣力度，1..100）/ fixed（固定金额，单位分）。
	DiscountType  string `form:"discountType" json:"discountType"`
	DiscountValue int64  `form:"discountValue" json:"discountValue"`
	// MinSubtotal 门槛（分）：小计低于它不能用。
	MinSubtotal int64 `form:"minSubtotal" json:"minSubtotal"`
	// MaxUses 总可用次数，0 = 不限。
	MaxUses int `form:"maxUses" json:"maxUses"`
	// PerUserLimit 每人可用次数，0 = 不限（匿名下单统计不到人，按不限口径）。
	PerUserLimit int `form:"perUserLimit" json:"perUserLimit"`
	// StartsAt / EndsAt 生效时间窗（文本，空 = 不限）。接受 2006-01-02 与
	// 2006-01-02 15:04(:05) 两种写法（后台表单用前者，接口调用方常用后者）。
	StartsAt string `form:"startsAt" json:"startsAt"`
	EndsAt   string `form:"endsAt" json:"endsAt"`
	// Status 1 启用 / 0 停用。停用不删：历史核销记录还要读它。
	Status int    `form:"status" json:"status"`
	Remark string `form:"remark" json:"remark"`

	// 操作人由 inbound 覆盖写入，客户端不可伪造。
	OperatorID   uint64 `form:"-" json:"-"`
	OperatorName string `form:"-" json:"-"`
}

// CouponListReq 优惠码列表查询（各维度可组合）。
type CouponListReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Keyword   string `form:"keyword" json:"keyword"`
	// Status "" 全部 / enabled / disabled / expired / exhausted（后两者按时间与次数算出来）。
	Status string `form:"status" json:"status"`
	Offset int    `form:"offset" json:"offset"`
	Limit  int    `form:"limit" json:"limit"`
}

// CouponValidateReq 优惠码试算（纯读，不占用次数）。
type CouponValidateReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Code      string `form:"code" json:"code"`
	// Subtotal 订单小计（分）：折扣按它算，折后价不由客户端决定。
	Subtotal int64 `form:"subtotal" json:"subtotal"`
	// UserID 0 = 匿名访客（按人限次对匿名不生效）。
	//
	// 口径修正（BIZ-10）：此前这里写着「由调用方从**会话 / 片段身份**写入」，
	// 而**没有任何调用方写它** —— 唯一入口是后台的 `GET /api/order/coupon/validate`
	//（order_handle.go 只做 ShouldBindQuery，而本字段是 form:"-"），
	// 于是访客侧的按人限次判定当前恒按匿名处理。
	// 补访客券入口时，这里必须由片段层从访客会话显式赋值（客户端传了也不生效）。
	UserID uint64 `form:"-" json:"-"`
}

// CouponRedemptionListReq 核销记录查询。
type CouponRedemptionListReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	CouponID  uint64 `form:"couponId" json:"couponId"`
	Code      string `form:"code" json:"code"`
	OrderID   uint64 `form:"orderId" json:"orderId"`
	Offset    int    `form:"offset" json:"offset"`
	Limit     int    `form:"limit" json:"limit"`
}

// FindOrderReq 按「一个线索」查订单（AI 工具的只读入口）。
//
// 与 ListOrderReq 分开而不是加字段：后者是列表页的入参（带 offset/limit 分页与
// 全站状态计数），而这里要的是「用户说了个线索（单号 / 客户名 / 邮箱 / 状态 / 时间段），
// 把它指向的订单找出来」。混在一体会让列表页的筛选语义与工具语义互相将就。
type FindOrderReq struct {
	ProjectID string `json:"projectId"`
	// Keyword 单号 / 客户邮箱 / 客户姓名，三者任一命中即算（由 model 层的 ILIKE 收口）。
	Keyword string `json:"keyword"`
	Status  string `json:"status"`
	// From / To 都是 YYYY-MM-DD 的**日界**（UTC），可选；两个要么都给要么都不给 ——
	// 只给一边的半开窗口说不清是「从这天起」还是「到这天止」。
	From  string `json:"from"`
	To    string `json:"to"`
	Limit int    `json:"limit"`
}

// FindOrderResp 找到的订单（不含全站状态计数 —— 那是列表页的东西，工具不需要）。
type FindOrderResp struct {
	List  []*OrderResp `json:"list"`
	Total int64        `json:"total"`
	// From / To 回显**实际生效**的窗口：调用方给了非法日期时这里是空的，
	// 而不是把用户给的原样抄回来（那会让「已按 X 到 Y 查」这句话变成假话）。
	From string `json:"from"`
	To   string `json:"to"`
}
