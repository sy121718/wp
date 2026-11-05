package text

import (
	"reflect"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/safepath"
	"go_wp/pkg/view/jet"
)

const AdminHomePath = "/admin"

func Inject(c *gin.Context, data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}

	// 取词函数只有一份组装点（见 Scope）：页面数据 map 与渲染器 VarMap 取同一份，
	// 两条出口才有可能给出同一句译文。
	scope := Scope(c)
	lang := scope.Lang
	t := scope.Translate

	data["lang"] = lang
	data["t"] = t
	data["tf"] = scope.TranslateArgs
	data["tn"] = scope.TranslateNamed
	data["langs"] = jet.LanguageOptions(lang)

	data["req_path"] = RequestURI(c)
	data["lang_redirect"] = LangRedirect(c)

	if title, ok := data["title"].(string); ok && title != "" {
		data["title"] = t(title, title)
	}

	seedNested(data, map[string]any{
		"lang": lang, "t": t, "tf": scope.TranslateArgs, "tn": scope.TranslateNamed,
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
