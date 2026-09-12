package maildto

// mail_tracking.go — 追踪载荷（#38 P1）。
//
// 放在 dto 而不是 service：contract 要暴露追踪能力，而 contract 不能引用 service
//（方向反了就成环）。载荷是不可变数据，本来也属于 dto。

// TrackPayload 追踪载荷（由签名 token 携带）。
type TrackPayload struct {
	LogID      uint64
	CampaignID uint64
	ContactID  uint64
	// URL 仅点击追踪使用；打开与退订为空。
	URL string
}
