package sysconfigdto

// SystemSaveReq saves the two system settings groups in one operation.
// Versions belong to the submitted snapshot, never the latest read snapshot.
type SystemSaveReq struct {
	DefaultLang     string `json:"defaultLang" binding:"required"`
	SiteLangURLMode string `json:"siteLangURLMode" binding:"required"`
	DefaultCountry  string `json:"defaultCountry" binding:"required"`
	DefaultCurrency string `json:"defaultCurrency" binding:"required"`
	I18NVersion     int64  `json:"i18nVersion" binding:"required,gt=0"`
	TradeVersion    int64  `json:"tradeVersion" binding:"required,gt=0"`
}
