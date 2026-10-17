package maildto

// TrackPayload 追踪载荷（由签名 token 携带）。
type TrackPayload struct {
	LogID      uint64
	CampaignID uint64
	ContactID  uint64
	// URL 仅点击追踪使用；打开与退订为空。
	URL string
}
