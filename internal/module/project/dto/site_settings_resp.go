package projectdto

import (
	json "go_wp/pkg/jsonx"
)

type SiteSettings struct {
	SiteName string `json:"siteName,omitempty"`

	SiteDesc string `json:"siteDesc,omitempty"`

	ContactEmail string `json:"contactEmail,omitempty"`

	GA4MeasurementID string `json:"ga4MeasurementId,omitempty"`

	SearchConsoleVerification string `json:"searchConsoleVerification,omitempty"`

	HeadScripts string `json:"headScripts,omitempty"`

	BodyScripts string `json:"bodyScripts,omitempty"`

	IndexNowKey string `json:"indexNowKey,omitempty"`

	NotFoundHTML string `json:"notFoundHtml,omitempty"`

	URLPatterns map[string]string `json:"urlPatterns,omitempty"`

	LangURLMode string `json:"langURLMode,omitempty"`

	ShippingBaseFee int64 `json:"shippingBaseFee,omitempty"`

	ShippingFreeThreshold int64 `json:"shippingFreeThreshold,omitempty"`
}

func ParseSiteSettings(raw json.RawMessage) SiteSettings {
	var s SiteSettings
	if len(raw) == 0 {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}
