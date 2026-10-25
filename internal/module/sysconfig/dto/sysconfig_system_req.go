package sysconfigdto

// SystemSaveReq saves the two system settings groups in one operation.
// Versions belong to the submitted snapshot, never the latest read snapshot.
type SystemSaveReq struct {
	DefaultLang     string `json:"defaultLang" form:"defaultLang" binding:"required"`
	SiteLangURLMode string `json:"siteLangURLMode" form:"siteLangURLMode" binding:"required"`
	DefaultCountry  string `json:"defaultCountry" form:"defaultCountry" binding:"required"`
	DefaultCurrency string `json:"defaultCurrency" form:"defaultCurrency" binding:"required"`
	I18NVersion     int64  `json:"i18nVersion" form:"i18nVersion" binding:"required,gt=0"`
	TradeVersion    int64  `json:"tradeVersion" form:"tradeVersion" binding:"required,gt=0"`
}
