package sysconfigdto

type SystemSettingsPageResp struct {
	Title string

	I18N  any
	Trade any

	LangOptions     []any
	CurrencyOptions []any
	CountryOptions  []any
	ModeOptions     []any

	LoadErr string
}
