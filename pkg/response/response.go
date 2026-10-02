package response

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"go_wp/pkg/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/sitehttps"

	"github.com/gin-gonic/gin"
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func Success(c *gin.Context, data ...interface{}) {
	var responseData interface{}
	if len(data) > 0 {
		responseData = data[0]
	}

	c.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: translate(c, enums.MsgOperationSuccess),
		Data:    responseData,
	})
}

func SuccessWithMessage(c *gin.Context, message string, data ...interface{}) {
	var responseData interface{}
	if len(data) > 0 {
		responseData = data[0]
	}

	c.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: translate(c, message),
		Data:    responseData,
	})
}

func ErrorWithMessage(c *gin.Context, code int, message string) {
	c.JSON(code, Response{
		Code:    code,
		Message: translate(c, message),
	})
}

// msgInternalError 500 响应的统一对外文案。
const msgInternalError = "服务器内部错误，请稍后重试"

// ErrorInternal 收敛服务端内部错误：完整错误只进日志，对外统一返回通用文案。
//
// handler 直接写 ErrorWithMessage(c, 500, err.Error()) 会把 SQL 片段、文件路径、
// 内部标识符原样返回给客户端 —— 既是信息泄漏，也违反「响应文案统一走 enums」的
// 约定。收敛到本函数后，泄漏面只剩日志，且不再依赖每个 handler 自觉。
func ErrorInternal(c *gin.Context, scene string, err error) {
	if err != nil {
		if scene == "" {
			scene = "http"
		}
		logger.Scene(scene).Error(err, "handler 内部错误")
	}
	ErrorWithMessage(c, http.StatusInternalServerError, msgInternalError)
}

// businessErrKey 匹配「业务错误」的消息形态：`模块.类别.语义`，例如 cart.err.outOfStock。
//
// **类别本身可以是多级**（`模块.类别.子类别.语义`）：`admin.navigation_translations.err.rowCountMismatch`
// 与 `admin.product_edit.err.defaultPriceInvalid` 都把类别写成 `xxx.err` 两级，全仓 33 个此类值。
// 因此判据是「三段起，可更长」而不是「恰好三段」：原分支只认恰好三段，把这 33 个里的 7 个判成
// 「不命中任何判据」，而它们的词条都在库里、运行时并不会退化成 500 —— 那是**判据的假阳性**，
// 不是命名违约。放宽到多段没有削弱它守的东西：内部错误（fmt.Errorf / SQL / os）不长成
// 点分小写的形状，多一级类别也不改变这个结论。
//
// 为什么用**形态**而不是错误类型来判定：项目的业务错误由 enums 常量构造，
// 那些常量本身就是 i18n key（见各模块 enums 包），而 Go 的内部错误（`fmt.Errorf`、
// 驱动返回的 SQL 错误、os 的路径错误）天然长不成这个形状。
// 类型判定的前提是 service 层处处用同一个包装类型 —— 那是几百处改动、且漏一处就静默失效；
// 形态判定是零成本的，且漏判的后果是「内部错误被当成业务错误展示」，
// 而那种 string 恰好长得像 key 的概率极低。
// **这一处与 pkg/CLAUDE.md 的约定存在张力，是刻意的取舍，记录在此**：
// 该约定写明「pkg/response 不做 i18n 翻译、不维护字符串错误码、不在 pkg 内扩散业务语义中转」。
// 本函数是审计条目 CQ-010 的 remediation 明确要求的形状（原文即 response.ErrorAuto(c, err)），
// 它把「错误是否可对外展示」这个判断从每个 handler 的自觉收敛到一处。
// 权衡：不收敛的话，泄漏面等于全部 handler 的自觉程度（几百处），而漏一处不会让测试变红；
// 收敛的代价是 pkg/response 知道了业务错误的**形态**（不是具体错误码，也不引入 enums 依赖）。
// 两害相权取收敛 —— 但如果你要恢复约定，删掉 ErrorAuto/IsBusinessError 即可，
// 调用点改回 ErrorInternal 或明写 ErrorWithMessage，不会有隐藏耦合。
var businessErrKey = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.[a-zA-Z0-9_]+(\.[a-zA-Z0-9_]+)*(\|.*)?$`)

// businessErrConstant 未迁形态的 enums 常量名形态：ErrAttachmentNotFound / MsgListSuccess。
//
// 为什么需要这一层：CQ-010 把 170 处 handler 换成 ErrorAuto 时，只有 cart / mail / order / user
// 四个模块的 enums 迁到了 key 形态（`模块.err.语义`），其余模块仍是「常量名即消息」——
// media 的 `ErrAttachmentNotFound = "ErrAttachmentNotFound"`、inventory 的
// `ErrSourceFilterInvalid = "ErrSourceFilterInvalid"`。判据只认 key 形态时，这些模块的**全部**
// 业务错误都会被判成内部错误 → 一律 500 + 通用文案，与 CQ-010 自己声明的「业务错误透出消息」
// 恰好相反。这一层把常量名形态补上（全仓 429 个此类常量），形态判定零成本且不需要改动 170 处调用。
var businessErrConstant = regexp.MustCompile(`^(Err|Msg)[A-Z][A-Za-z0-9]*$`)

// IsBusinessError 判断一个错误的消息是否是业务错误（可对外展示）。
//
// 判据只有两层，两者都是**形态确定**的（不依赖字符串特征的启发式）：
//  1. key 形态 `模块.类别.语义`（已迁模块，如 cart.err.outOfStock）；参数化协议（key|param）
//     也算业务错误 —— 翻译层会把 | 后面的部分当参数填进模板；
//  2. enums 常量名形态 `ErrXxx` / `MsgXxx`（仍是「常量名即消息」的模块，如 ErrSourceFilterInvalid）。
//
// **第 3 层（「纯中文短文案」）已删除**，原因留在这里：它用「含汉字 + 不含一份内部错误特征清单」
// 去猜语义，而那份清单永远不全 —— 每加一条特征，都是在用一个字符串的巧合区分两种语义。
// 删层的代价已由 masterdata 的常量迁移承担（值从 `"参数不合法"` 改成 `masterdata.err.invalidParam`，
// 词条见迁移 198）；迁完之后全仓 enums 的 Err* / Msg* 常量都落在这两层里，判据不再有猜的成分。
// 回归判据见 TestIsBusinessErrorCoversAllModuleEnums：它逐个校验各模块 enums 的常量值是否命中
// 这两层，新增一个「值既不是 key 也不是常量名」的常量会直接变红，而不是静默变成 500。
//
// 为什么用形态而不是错误类型：类型判定的前提是 service 层处处用同一个包装类型，那是几百处改动、
// 且漏一处即静默失效。形态判定的漏判方向是**安全的**：形状不认识就按内部错误处理
// （500 + 通用文案 + 记日志），而不是把内部细节透出去。
func IsBusinessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return businessErrKey.MatchString(msg) || businessErrConstant.MatchString(msg)
}

// ErrorAuto 按错误性质决定对外文案：业务错误透出消息，内部错误只记日志并返回通用提示。
//
// 这是 handler 的默认选择 —— 直接写 ErrorWithMessage(c, 400, err.Error()) 看起来无害，
// 但只要 service 里有一处把 SQL 错误或文件路径原样返回，它就会出现在客户端响应里，
// 而那种泄漏不会让测试变红。判据集中在 IsBusinessError 一处，
// 新增业务错误只要按 enums 的 key 形态写就自动被认作可展示。
//
// scene 用于日志定位（如 "presentation"），业务错误路径不记日志（它是预期内的分支）。
func ErrorAuto(c *gin.Context, code int, scene string, err error) {
	if IsBusinessError(err) {
		ErrorWithMessage(c, code, err.Error())
		return
	}
	if err != nil {
		if scene == "" {
			scene = "http"
		}
		logger.Scene(scene).Error(err, "handler 内部错误")
	}
	// 内部错误一律 500 而不是沿用调用方给的 code —— 调用方通常写的是 400/404，
	// 那是按「预期内的业务失败」选的；既然实际是内部错误，状态码就该如实地是 500，
	// 否则客户端与监控都会把内部故障当成参数问题。
	ErrorWithMessage(c, http.StatusInternalServerError, msgInternalError)
}

func ParamError(c *gin.Context, msg ...string) {
	message := enums.ErrInvalidParams
	if len(msg) > 0 {
		message = msg[0]
	}

	ErrorWithMessage(c, http.StatusBadRequest, message)
}

func NotFound(c *gin.Context, msg ...string) {
	message := enums.ErrNotFound
	if len(msg) > 0 {
		message = msg[0]
	}

	ErrorWithMessage(c, http.StatusNotFound, message)
}

func SuccessWithData(c *gin.Context, data interface{}) {
	Success(c, data)
}

// TranslateMessage 将业务消息按请求语言翻译（translate 的公开出口）。
// 供中间件等需要直接构造 response.Response 的场景使用（如 AbortWithStatusJSON）。
func TranslateMessage(c *gin.Context, message string) string {
	return translate(c, message)
}

// LangCookieName 语言协商 Cookie 名（后台语言切换写入，requestLanguage 优先读取）。
const LangCookieName = "lang"

// langCookieMaxAge 语言 Cookie 有效期（秒，1 年）。
const langCookieMaxAge = 365 * 24 * 60 * 60

// RequestLanguage 返回请求语言（requestLanguage 的公开出口），供 service 层按语言处理（如菜单标题翻译）。
func RequestLanguage(c *gin.Context) string {
	return requestLanguage(c)
}

// NormalizeLang 校验并规范化语言代码（语言切换入口复用同一白名单，避免第二套规则）。
// 返回规范化语言码与是否受支持；非法输入返回默认语言 + false，绝不返回空串（调用方无需再兜底）。
func NormalizeLang(raw string) (string, bool) {
	fallback := i18n.GetDefaultLang()
	if lang := normalizeLang(raw, ""); lang != "" {
		return lang, true
	}
	return fallback, false
}

// SetLangCookie 写入语言协商 Cookie：Path=/、HttpOnly、SameSite=Lax，
// release 模式（gin.ReleaseMode，由 server.mode 驱动）自动加 Secure。
// SameSite=Lax 下同站导航与 HTMX（同源 XHR）请求都会携带该 Cookie，故 HTMX 请求同样能带上语言。
func SetLangCookie(c *gin.Context, lang string) {
	if c == nil {
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     LangCookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   langCookieMaxAge,
		HttpOnly: true,
		Secure:   sitehttps.Enabled(),
		SameSite: http.SameSiteLaxMode,
	})
}

// requestLanguage 解析请求语言：Cookie lang 优先，其次 query lang，再次 Accept-Language 首段，最后默认语言。
// 解析结果统一规范化（zh/en 大小写与区域变体 → 标准码，不支持的语言回退默认，见 i18n-issues 2-4）。
// 兜底：任何一层缺失或非法都继续降级，最终返回默认语言，绝不报错、绝不 panic（c 为 nil 亦安全）。
func requestLanguage(c *gin.Context) string {
	fallback := i18n.GetDefaultLang()

	if c == nil {
		return fallback
	}

	// 1) Cookie（语言切换落地的持久选择）
	if cookie, err := c.Cookie(LangCookieName); err == nil {
		if lang := strings.TrimSpace(cookie); lang != "" {
			return normalizeLang(lang, fallback)
		}
	}

	// 2) query lang（显式单次覆盖）
	if lang := strings.TrimSpace(c.Query("lang")); lang != "" {
		return normalizeLang(lang, fallback)
	}

	// 3) Accept-Language 首段（浏览器默认偏好）
	accept := strings.TrimSpace(c.GetHeader("Accept-Language"))
	if accept != "" {
		first := strings.TrimSpace(strings.Split(accept, ",")[0])
		if idx := strings.Index(first, ";"); idx > 0 {
			first = strings.TrimSpace(first[:idx])
		}
		if first != "" {
			return normalizeLang(first, fallback)
		}
	}

	// 4) 配置默认语言
	return fallback
}

// normalizeLang 把语言代码规范化为系统支持的格式：
// zh / zh-cn / zh-Hans 等 → zh-CN；en / en-US / en-GB 等 → en-US；
// 超长或未识别的输入回退默认语言，避免以无效 code 命中不了资源。
func normalizeLang(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 10 {
		return fallback
	}

	lower := strings.ToLower(raw)
	switch {
	case lower == "zh" || lower == "zh-cn" || lower == "zh_cn" || lower == "zh-hans":
		return "zh-CN"
	case lower == "en" || lower == "en-us" || lower == "en_us" || lower == "en-gb":
		return "en-US"
	default:
		return fallback
	}
}

// translate 将业务消息按请求语言翻译，支持三种形态：
//  1. 纯 key：message 即 sys_i18n 资源 key，命中直接翻译；
//  2. key|param：带格式化参数协议（如 "ErrAccountLocked|5m0s"），翻译后 Sprintf 注入参数；
//  3. key: detail：拼接消息兼容（如 "ErrInvalidParams: xxx"），仅翻译 key 前缀。
//
// 均未命中时原样返回，保证改造窗口期（库缺资源）不出现异常输出。
func translate(c *gin.Context, message string) string {
	lang := requestLanguage(c)

	// 形态 1：纯 key
	if text := i18n.GetText(message, lang); text != message {
		return text
	}

	// 形态 2：key|param1|param2
	if idx := strings.Index(message, "|"); idx > 0 {
		key := message[:idx]
		if text := i18n.GetText(key, lang); text != key {
			// 协议限定：模板只允许 %s / %[n]s / %%（参数按字符串注入）。
			// 含 %d/%f 等协议外占位符时不做注入，按 key 原文降级，避免 Go 格式错误输出。
			if !i18n.HasStringPlaceholdersOnly(text) {
				return message
			}
			args := strings.Split(message[idx+1:], "|")
			anyArgs := make([]interface{}, len(args))
			for i, a := range args {
				anyArgs[i] = a
			}
			return fmt.Sprintf(text, anyArgs...)
		}
	}

	// 形态 3：key: detail
	if idx := strings.Index(message, ": "); idx > 0 {
		prefix := message[:idx]
		if text := i18n.GetText(prefix, lang); text != prefix {
			return text + message[idx:]
		}
	}

	return message
}
