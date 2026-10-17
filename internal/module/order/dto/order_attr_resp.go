package orderdto

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
	// First 首次触点的归因（first-touch 口径）。
	//
	// 上面那组 SourceType / Referrer / UTM 描述的是**本次会话**（last-touch 口径）；
	// 获客分析要的是**他第一次怎么来的** —— 同一个人可能先点了广告、隔几天搜品牌词才下单，
	// 两个口径给出完全不同的答案。Sourcebuster 因此写两份 cookie（sbjs_current / sbjs_first），
	// 而 WooCommerce 核心只把 current 那份映射进了 _wc_order_attribution_*。
	First FirstTouch `json:"first"`

	// Landing 首次落地页（与 Session.Entry 的区别：Entry 是本次会话入口，
	// Landing 是访客第一次到站的那一页 —— 多会话场景下两者不同）。
	Landing string `json:"landing"`
	// Trail 下单前若干分钟内浏览过的页面（按时间升序）。
	// WC 只存了 session_pages 这个**页数**，存不下「看过哪几页」；
	// 纠纷与转化分析要的恰恰是后者。
	Trail []TrailPage `json:"trail"`
}

// FirstTouch 首次触点（对标 Sourcebuster 的 sbjs_first）。
type FirstTouch struct {
	SourceType string  `json:"sourceType"`
	Referrer   string  `json:"referrer"`
	UTM        UTMInfo `json:"utm"`
	// Landing 首次落地页。
	Landing string `json:"landing"`
	// At 首次到站时间（RFC3339）。
	At string `json:"at"`
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
