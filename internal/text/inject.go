package text

// inject.go — 模板数据的取词键注入（lang / t / tf / langs / lang_redirect）。
//
// 为什么由 Go 侧注入而不是给模板注册全局函数：语言是**请求级**的，Set.AddGlobal 的全局
// 函数拿不到当前请求的语言，按请求切语言就得改共享状态（并发下 -race 报 DATA RACE）。
//
// 漏注入的后果是**静默**的：模板里 tr := .["t"] 拿到 nil，该模板所有取词一起变空串 ——
// 页面结构完整、状态码 200、日志干净（见 internal/templates/CLAUDE.md 的实测记录）。
// 所以整页（adminui.Prepare）与片段（本函数）都必须走这里，不能各写一份。

import (
	"fmt"
	"reflect"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
)

// AdminHomePath 控制面首页：语言回跳与拒绝页的兜底落点。
const AdminHomePath = "/admin"

// Inject 为模板数据补上取词键，返回同一个 map（便于链式写）。
//
// 注入：lang / t（func(key, fallback)）/ tf（func(key, fallback, args []string)）/
// langs（语言切换候选）/ lang_redirect（语言切换表单的 redirect 隐藏域）；
// data["title"] 是 i18n key 时顺带翻译。
//
// data 为 nil 时新建一个 —— 调用方常常是「先建 map 再逐键填」的形态。
func Inject(c *gin.Context, data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}

	lang := Lang(c)
	t := templates.TranslateFunc(lang)

	data["lang"] = lang
	data["t"] = t
	// tf 是「取词 + %s 占位符填充」：签名 func(key, fallback string, args []string) string。
	// 需要它是因为结论文案带计数（「已删除 %s 个页面。」），而 Go 侧不再拼整句。
	tfFn := func(key, fallback string, args []string) string {
		return response.ArgsText(c, key, fallback, args)
	}
	data["tf"] = tfFn
	// tn 是「取词 + {name} 具名占位符填充」：签名 func(key, fallback string, args map[string]any) string。
	// 英文与中文的语序不同（「{n} 个」/「{n} tags」），具名占位符让词条自己决定位置。
	tnFn := func(key, fallback string, args map[string]any) string {
		params := make(map[string]string, len(args))
		for k, v := range args {
			params[k] = fmt.Sprint(v)
		}
		return i18n.FillTranslate(t, key, fallback, params)
	}
	data["tn"] = tnFn
	data["langs"] = templates.LanguageOptions(lang)
	// req_path：当前请求的路径 + 查询串（不含 scheme/host）。分页条据此拼翻页链接 ——
	// 链接基址是**请求事实**，不是页面装配的产物，所以由注入层给，控制器不再算。
	data["req_path"] = RequestURI(c)
	data["lang_redirect"] = LangRedirect(c)

	// 页面标题：enums 常量已是 key（如 MsgPagesTitle），此处按当前语言翻译；
	// 非 key 的字面量标题（如登录页）走 fallback 原样返回，不改变行为。
	if title, ok := data["title"].(string); ok && title != "" {
		data["title"] = t(title, title)
	}
	// 嵌套的 include 数据（抽屉 / 片段 / 行内表单）同样是模板数据根：include 会换掉 . 的指向，
	// 那些 map 里的 .["t"] 只能由渲染层给。递给模板的每一张 map 都补同一套键 ——
	// 形状由 service 出，取词函数由渲染层出，控制器不再为了一个取词函数去搬数据。
	seedNested(data, map[string]any{
		"lang": lang, "t": t, "tf": tfFn, "tn": tnFn,
		"langs": data["langs"], "lang_redirect": data["lang_redirect"], "req_path": data["req_path"],
	}, 1)
	return data
}

// seedNestedDepth 嵌套注入的最大深度（页面 → 行 → 片段 → 行内片段已到 6；留一倍余量）。
const seedNestedDepth = 8

// seedNested 给嵌套的 string-key map 补渲染期键（缺什么补什么，绝不覆盖已有值）。
//
// 判据是**键类型**而不是具体类型：gin.H / map[string]any / 具名 map 都是模板数据根；
// 切片里的元素同样处理（行集合里的抽屉数据就是这一形）。结构体不处理 —— 行元素是 dto，
// 模板按字段名读，不需要取词函数。
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
			// 强类型 map（map[string]XxxResp）塞不进函数值：只补元素类型是 interface 的 map，
			// 模板数据根都是 map[string]any / gin.H 这一形。
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
		// 行元素是 dto 结构体：模板按字段名读它们，但结构体里的**片段数据根**（抽屉的
		// EditForm / ValuesForm）仍是 map，同样要拿到取词函数 —— 只看顶层 map 会漏掉它们，
		// 症状是 include 里 .["t"] 缺键，取词调用在渲染期 panic。
		for i := 0; i < rv.NumField(); i++ {
			field := rv.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			seedNested(rv.Field(i).Interface(), seed, depth+1)
		}
	}
}

// LangRedirect 语言切换表单 redirect 隐藏域的取值。
//
// 判据只有一份，在 pkg/safepath.LangRedirectPath：站内相对路径且不超过 MaxReturnPathBytes
// 才通过。本函数只补渲染侧的两级兜底 —— 丢弃整个 URI（含超长 query）回落到当前请求的
// **路径**；路径本身也异常时回控制面首页。
func LangRedirect(c *gin.Context) string {
	if c == nil {
		return AdminHomePath
	}
	if p := safepath.LangRedirectPath(RequestURI(c)); p != "" {
		return p
	}
	return requestPathOrAdminHome(c)
}

// requestPathOrAdminHome 回落到当前请求的路径（丢掉 query）：回跳仍回到发起页；
// 路径本身异常（空 / 非同源 / 超长）时回控制面首页。
func requestPathOrAdminHome(c *gin.Context) string {
	if c != nil && c.Request != nil && c.Request.URL != nil {
		if p := c.Request.URL.Path; safepath.IsSameOriginPath(p) && len(p) <= safepath.MaxReturnPathBytes {
			return p
		}
	}
	return AdminHomePath
}

// RequestURI 当前请求的站内 URI（含 query）；异常时回控制面首页。
//
// 日志与语言回跳都要它：日志要真实 URI，语言回跳要经 LangRedirect 收敛后的值 ——
// 两者不能共用一份（拿日志值当回跳目标就是开放重定向）。
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
