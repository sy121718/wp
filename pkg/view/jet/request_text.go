package jet

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
)

// 本文件是「请求语言 → 模板取词」的接线层：原 internal/text 已并入 jet。
//
// 它只做接线，不重复实现取词与回跳判据：
// 语言来源归 response.RequestLanguage，取词实现归 jet.TranslateFunc，
// 安全回跳归 pkg/safepath，错误事实归 pkg/facing。

// AdminHomePath 是应用回跳的默认落点；调用方也可显式传自己的首页。
const AdminHomePath = "/admin"

const (
	InternalKey      = response.InternalKey
	InternalFallback = response.InternalFallback
)

var (
	userIDMu sync.RWMutex
	userIDOf func(*gin.Context) int64
)

// SetRequestUserID 由应用装配提供当前请求的主体 ID；
// 框架的错误日志只问这个回调，不反向依赖具体的会话中间件。
func SetRequestUserID(fn func(*gin.Context) int64) {
	userIDMu.Lock()
	defer userIDMu.Unlock()
	userIDOf = fn
}

func requestUserID(c *gin.Context) int64 {
	userIDMu.RLock()
	fn := userIDOf
	userIDMu.RUnlock()
	if fn == nil {
		return 0
	}
	return fn(c)
}

// Lang 返回本次请求的语言（cookie > query > Accept-Language）。
func Lang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// RequestTranslateFunc 返回绑定本次请求语言的 (key, fallback) 取词函数。
//
// 与 TranslateFunc(lang) 的区别：这个读请求语言，那个由调用方给定语言。
func RequestTranslateFunc(c *gin.Context) func(key, fallback string) string {
	return TranslateFunc(Lang(c))
}

// Scope 按请求组装一次模板取词上下文。
//
// 每次调用都返回新闭包，闭包里捕获的是本次请求的 lang 与 gin.Context：
// 它不写进任何进程级容器，所以并发请求之间不会互相改语言。
func Scope(c *gin.Context) RenderScope {
	lang := Lang(c)
	t := TranslateFunc(lang)

	tf := func(key, fallback string, args []string) string {
		return response.ArgsText(c, key, fallback, args)
	}

	tn := func(key, fallback string, args map[string]any) string {
		params := make(map[string]string, len(args))
		for k, v := range args {
			params[k] = fmt.Sprint(v)
		}
		return i18n.FillTranslate(t, key, fallback, params)
	}

	return RenderScope{
		Lang:           lang,
		Translate:      t,
		TranslateArgs:  tf,
		TranslateNamed: tn,
	}
}

// AttachScope 把本次请求的取词上下文挂到 ResponseWriter 上，
// 供渲染器在 Execute 时作为 VarMap 读取（见 ScopeWriter）。
//
// 它装配在模板渲染中间件里，因此对「控制器直接 c.HTML」与
// 「pkg/view、internal/adminui 渲染」两条合法出口一视同仁。
func AttachScope(c *gin.Context) {
	if c == nil {
		return
	}
	WithRenderScope(c, Scope(c))
}

// FactOf 把错误转成对外可展示的事实；非业务错误只给归口文案并把原文写日志。
func FactOf(c *gin.Context, scene string, err error) facing.Fact {
	if err == nil {
		return facing.Fact{}
	}
	if fact, ok := facing.FactOf(err); ok {
		return fact
	}
	logger.Scene(scene).
		With("path", RequestURI(c)).
		With("user_id", requestUserID(c)).
		Error(err, "操作失败（非业务错误，只对外给归口文案）")
	return facing.Fact{Key: InternalKey, Fallback: InternalFallback}
}

// RequestURI 返回当前请求的 URI；缺请求时回落到应用首页。
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

// LangRedirect 返回语言切换后可安全回跳的路径。
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

// Inject 给页面数据 map 补上取词函数与请求事实。
//
// 取词函数只有一份组装点（见 Scope）：页面数据 map 与渲染器 VarMap 取同一份，
// 两条出口才有可能给出同一句译文。已有键不覆盖。
func Inject(c *gin.Context, data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}

	scope := Scope(c)
	lang := scope.Lang
	t := scope.Translate

	data["lang"] = lang
	data["t"] = t
	data["tf"] = scope.TranslateArgs
	data["tn"] = scope.TranslateNamed
	data["langs"] = LanguageOptions(lang)

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
