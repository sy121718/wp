package text

import (
	"fmt"
	"reflect"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
)

const AdminHomePath = "/admin"

func Inject(c *gin.Context, data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}

	lang := Lang(c)
	t := templates.TranslateFunc(lang)

	data["lang"] = lang
	data["t"] = t

	tfFn := func(key, fallback string, args []string) string {
		return response.ArgsText(c, key, fallback, args)
	}
	data["tf"] = tfFn

	tnFn := func(key, fallback string, args map[string]any) string {
		params := make(map[string]string, len(args))
		for k, v := range args {
			params[k] = fmt.Sprint(v)
		}
		return i18n.FillTranslate(t, key, fallback, params)
	}
	data["tn"] = tnFn
	data["langs"] = templates.LanguageOptions(lang)

	data["req_path"] = RequestURI(c)
	data["lang_redirect"] = LangRedirect(c)

	if title, ok := data["title"].(string); ok && title != "" {
		data["title"] = t(title, title)
	}

	seedNested(data, map[string]any{
		"lang": lang, "t": t, "tf": tfFn, "tn": tnFn,
		"langs": data["langs"], "lang_redirect": data["lang_redirect"], "req_path": data["req_path"],
	}, 1)
	return data
}

const seedNestedDepth = 8

func seedNested(v any, seed map[string]any, depth int) {
	if depth > seedNestedDepth || v == nil || len(seed) == 0 {
		return
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String || rv.IsNil() {
			return
		}
		keyType, elemType := rv.Type().Key(), rv.Type().Elem()
		for name, value := range seed {
			key := reflect.ValueOf(name).Convert(keyType)
			if rv.MapIndex(key).IsValid() {
				continue
			}

			if elemType.Kind() != reflect.Interface {
				continue
			}
			rv.SetMapIndex(key, reflect.ValueOf(value))
		}
		for _, key := range rv.MapKeys() {
			seedNested(rv.MapIndex(key).Interface(), seed, depth+1)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			seedNested(rv.Index(i).Interface(), seed, depth+1)
		}
	case reflect.Pointer:
		if rv.IsNil() {
			return
		}
		seedNested(rv.Elem().Interface(), seed, depth+1)
	case reflect.Struct:

		for i := 0; i < rv.NumField(); i++ {
			field := rv.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			seedNested(rv.Field(i).Interface(), seed, depth+1)
		}
	}
}

func LangRedirect(c *gin.Context) string {
	if c == nil {
		return AdminHomePath
	}
	if p := safepath.LangRedirectPath(RequestURI(c)); p != "" {
		return p
	}
	return requestPathOrAdminHome(c)
}

func requestPathOrAdminHome(c *gin.Context) string {
	if c != nil && c.Request != nil && c.Request.URL != nil {
		if p := c.Request.URL.Path; safepath.IsSameOriginPath(p) && len(p) <= safepath.MaxReturnPathBytes {
			return p
		}
	}
	return AdminHomePath
}

func RequestURI(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return AdminHomePath
	}
	uri := c.Request.URL.RequestURI()
	if uri == "" {
		return AdminHomePath
	}
	return uri
}
