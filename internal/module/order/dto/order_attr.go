package orderdto

// order_attr.go — 归因与轨迹快照（orders.attribution 这一 JSONB 列的**形状定义**）。
//
// 字段名与语义对照 WooCommerce 核心的 _wc_order_attribution_* 系列，以及它背后的
// Sourcebuster.js（wp 侧的 sbjs_* cookie 就是它写的）。参考的是**字段清单**，
// 不是它的存法：WC 把这些拆成十几行 postmeta，这里集中在一列 JSON 里 —— 它们只用于
// 分析与对账，不参与业务条件查询，拆表只会长出一张 key-value 表。
//
// 为什么必须**冗余**（而不是下单时去关联查）：归因数据天生是快照且会变 ——
// cookie 会过期、UTM 参数会被随手改、访客下次来的来源可能完全不同。订单要留下的是
// **下单那一刻看到的那份**，事后任何变动都不该改写历史订单的来源。

// Attribution 归因与轨迹快照。
type Attribution struct {
	// SourceType 来源类型：utm / referral / typein / organic（对齐 WC 的同名字段）。
	SourceType string `json:"sourceType"`
	// Referrer 引荐来源 URL（SourceType=referral 时才有意义）。
	Referrer string      `json:"referrer"`
	UTM      UTMInfo     `json:"utm"`
	Ad       AdInfo      `json:"ad"`
	Session  SessionInfo `json:"session"`
	Device   DeviceInfo  `json:"device"`
	// Landing 首次落地页（与 Session.Entry 的区别：Entry 是本次会话入口，
	// Landing 是访客第一次到站的那一页 —— 多会话场景下两者不同）。
	Landing string `json:"landing"`
	// Trail 下单前若干分钟内浏览过的页面（按时间升序）。
	// WC 只存了 session_pages 这个**页数**，存不下「看过哪几页」；
	// 纠纷与转化分析要的恰恰是后者。
	Trail []TrailPage `json:"trail"`
}

// UTMInfo 广告与渠道参数（Sourcebuster 的完整 UTM 家族，WC 核心只用了前三个）。
type UTMInfo struct {
	Source          string `json:"source"`
	Medium          string `json:"medium"`
	Campaign        string `json:"campaign"`
	Content         string `json:"content"`
	Term            string `json:"term"`
	ID              string `json:"id"`
	Platform        string `json:"platform"`
	CreativeFormat  string `json:"creativeFormat"`
	MarketingTactic string `json:"marketingTactic"`
}

// AdInfo 广告平台点击 id。各家参数名不同，分开存是为了对账时不用猜是哪个平台。
type AdInfo struct {
	GCLID   string `json:"gclid"`   // Google Ads
	FBCLID  string `json:"fbclid"`  // Meta
	TTCLID  string `json:"ttclid"`  // TikTok
	MSCLKID string `json:"msclkid"` // Microsoft Ads
	// ClickID 自有渠道的点击 id（自己发的推广链接带的参数）。
	ClickID string `json:"clickId"`
}

// SessionInfo 会话事实。
type SessionInfo struct {
	// Entry 本次会话的入口页。
	Entry string `json:"entry"`
	// Pages 本次会话浏览的页数（对齐 WC 的 session_pages）。
	Pages int `json:"pages"`
	// Count 这是该访客的第几次会话（对齐 WC 的 session_count）。
	Count int `json:"count"`
	// StartTime 会话开始时间（RFC3339）。
	StartTime string `json:"startTime"`
	// DurationSeconds 会话时长（秒）。
	DurationSeconds int `json:"durationSeconds"`
}

// DeviceInfo 设备信息（对齐 WC 的 device_type / user_agent）。
type DeviceInfo struct {
	// Type desktop / mobile / tablet / bot。
	Type      string `json:"type"`
	UserAgent string `json:"userAgent"`
	Screen    string `json:"screen"`
}

// TrailPage 下单前浏览过的一个页面。
type TrailPage struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	// At 进入该页的时间（RFC3339）。
	At string `json:"at"`
	// Seconds 在该页停留的秒数（0 表示还没离开就下单了）。
	Seconds int `json:"seconds"`
}
