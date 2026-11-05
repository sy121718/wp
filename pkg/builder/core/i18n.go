package core

type I18nAware interface {
	ApplyI18n(text func(key, fallback string) string)
}
