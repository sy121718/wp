package orderdto

type Attribution struct {
	SourceType string `json:"sourceType"`

	Referrer string      `json:"referrer"`
	UTM      UTMInfo     `json:"utm"`
	Ad       AdInfo      `json:"ad"`
	Session  SessionInfo `json:"session"`
	Device   DeviceInfo  `json:"device"`

	First FirstTouch `json:"first"`

	Landing string `json:"landing"`

	Trail []TrailPage `json:"trail"`
}

type FirstTouch struct {
	SourceType string  `json:"sourceType"`
	Referrer   string  `json:"referrer"`
	UTM        UTMInfo `json:"utm"`

	Landing string `json:"landing"`

	At string `json:"at"`
}

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

type AdInfo struct {
	GCLID   string `json:"gclid"`
	FBCLID  string `json:"fbclid"`
	TTCLID  string `json:"ttclid"`
	MSCLKID string `json:"msclkid"`

	ClickID string `json:"clickId"`
}

type SessionInfo struct {
	Entry string `json:"entry"`

	Pages int `json:"pages"`

	Count int `json:"count"`

	StartTime string `json:"startTime"`

	DurationSeconds int `json:"durationSeconds"`
}

type DeviceInfo struct {
	Type      string `json:"type"`
	UserAgent string `json:"userAgent"`
	Screen    string `json:"screen"`
}

type TrailPage struct {
	URL   string `json:"url"`
	Title string `json:"title"`

	At string `json:"at"`

	Seconds int `json:"seconds"`
}
