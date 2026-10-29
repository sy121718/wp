package response

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/sitehttps"

	"github.com/gin-gonic/gin"
)

// ErrHandled 处理器已经把响应写完了（SSE 流、文件下载、302、空状态码），
// 上层的渲染出口看到它就**不要再写第二次**。
//
// 为什么是哨兵而不是「返回 nil 视图」：nil 视图是编程错误（视图恒非 nil），
// 两者混用会让「这页渲染崩了」被当成「这页自己写过了」静默吞掉。
var ErrHandled = errors.New("response already written")

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

	writeResponse(c, http.StatusOK, Response{
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

	writeResponse(c, http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: translate(c, message),
		Data:    responseData,
	})
}

func ErrorWithMessage(c *gin.Context, code int, message string) {
	writeResponse(c, code, Response{
		Code:    code,
		Message: translate(c, message),
	})
}

// ErrorWithData 带明细的错误：message 一句话，data 放**可定位的结构化条目**。
//
// 用于「整批被拒」这类结论：一句话说不清是哪几条不行，而把它们塞进 message 又会让文案
// 变成一段拼接的散文（既不可翻译、也没法让前端逐行展示）。判据是「这些数据是不是给人
// 照着办下一个动作用的」—— 是就用 data，不是就写进 message。
func ErrorWithData(c *gin.Context, code int, message string, data interface{}) {
	writeResponse(c, code, Response{
		Code:    code,
		Message: translate(c, message),
		Data:    data,
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
		logInternalError(c, scene, "handler 内部错误", err)
	}
	ErrorWithMessage(c, http.StatusInternalServerError, msgInternalError)
}

// IsBusinessError 判断一个错误的消息是否是业务错误（可对外展示）。
//
// **判据的唯一实现在 pkg/facing**（IsBusinessError / IsBusinessKey）—— 「什么叫业务错误」
// 是跨出口的约定（JSON 接口、后台页面、访客页、AI 工具判的是同一件事），放在本包会让
// 第二条出口自己再判一遍，两份判据漂移的症状是「同一条错误，接口给了具体原因、
// 页面上只说系统内部错误」，而两边都不报错。本函数保留为转发：JSON 出口的调用点有 50+ 处，
// 改名没有收益。
//
// 为什么用形态而不是错误类型：类型判定的前提是 service 层处处用同一个包装类型，那是几百处改动、
// 且漏一处即静默失效。形态判定的漏判方向是**安全的**：形状不认识就按内部错误处理
// （500 + 通用文案 + 记日志），而不是把内部细节透出去。
// 回归判据见 TestIsBusinessErrorCoversAllModuleEnums（逐个校验各模块 enums 的常量值是否命中）。
func IsBusinessError(err error) bool {
	return facing.IsBusinessError(err)
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
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		// 整批被拒的错误带可定位明细：明细进 data.items（逐条翻译），结论仍是一句话。
		// 判据是接口形状（facing.BlockedError），不是某个模块的具体类型 —— 出口不认识业务类型。
		if data, blocked := blockedDataOf(c, scene, err); blocked {
			ErrorWithData(c, code, businessText(c, scene, fact), data)
			return
		}
		ErrorWithMessage(c, code, businessText(c, scene, fact))
		return
	}
	if err != nil {
		if scene == "" {
			scene = "http"
		}
		logInternalError(c, scene, "handler 内部错误", err)
	}
	// 内部错误一律 500 而不是沿用调用方给的 code —— 调用方通常写的是 400/404，
	// 那是按「预期内的业务失败」选的；既然实际是内部错误，状态码就该如实地是 500，
	// 否则客户端与监控都会把内部故障当成参数问题。
	ErrorWithMessage(c, http.StatusInternalServerError, msgInternalError)
}

// 状态码分档的全仓口径（出口只读，不在出口重新判断）：
//
//	400 —— 参数不合法 / 业务拒绝（业务错误的默认档）
//	404 —— 找不到：声明处用 facing.WithStatus(err, 404)
//	409 —— 唯一性冲突、状态冲突、并发版本冲突：声明处用 facing.WithStatus(err, 409)
//	500 —— 未识别的错误（一定记日志，对外只给归口文案）
//
// 为什么不在出口按错误文案猜「含 NotFound 就是 404」：分档会跟着文案走 ——
// 改了措辞、换了语言，状态码就静默变成 400 或 500，而测试与日志都不报错。
// 谁声明错误，谁声明状态（见 facing.Statused）。
func ErrorStatus(err error) int {
	if status, ok := facing.StatusOf(err); ok {
		return status
	}
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// ErrorStatusAuto 出口：状态码由 ErrorStatus 定，文案由 ErrorAuto 定（同一份判据）。
//
// 业务错误的内部错误分支仍强制 500 —— ErrorAuto 会覆盖传入的 code。
func ErrorStatusAuto(c *gin.Context, scene string, err error) {
	ErrorAuto(c, ErrorStatus(err), scene, err)
}

// blockedDataOf 整批拒绝的明细：{"items":[{id,name,reasonKey,message}]}。
//
// data 是对象而不是裸数组：报文里还得留出别的位置（现在只有 items，将来加计数或整批
// 上下文都不用改形状），前端读 data.items 也不用区分两种类型。
func blockedDataOf(c *gin.Context, scene string, err error) (gin.H, bool) {
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) {
		return nil, false
	}
	items := blocked.BlockedItems()
	rows := make([]gin.H, 0, len(items))
	for _, it := range items {
		rows = append(rows, gin.H{
			"id":        it.ID,
			"name":      it.Name,
			"reasonKey": it.ReasonKey,
			"message":   businessText(c, scene, facing.Fact{Key: it.ReasonKey, Fallback: it.Fallback, Business: true}),
		})
	}
	return gin.H{"items": rows}, true
}

// businessText 事实 → 当前语言文案；i18n 命不中时退回**声明处给的兜底**。
//
// 两条出口（ErrorAuto / MessageOf）都走这一个函数：各写一份的失败形态是「接口给了原因、
// 页面只说服务器内部错误」或「页面显示中文、接口显示裸 key」，而两边都不报错。
func businessText(c *gin.Context, scene string, fact facing.Fact) string {
	out := translate(c, fact.Key)
	if out != fact.Key {
		// 复合形态（`key: 明细`）：译文在前、明细原样拼在后。
		// 明细是模块自造的可读片段（如「当前第 7 版」），不参与翻译；
		// 不拼它等于把「哪一版」这个唯一可行动的信息丢掉。
		if fact.Tail != "" {
			return out + "：" + fact.Tail
		}
		return out
	}
	// 声明处给了中文兜底（facing.New(key, "一句话") / enums.Err 工厂）→ 用兜底。
	if fact.Fallback != "" && fact.Fallback != fact.Key {
		return fact.Fallback
	}
	// 两样都没有：key 与兜底同形（errors.New("some.business.key") 这类裸 key）。
	// 交回 translate 的结果 —— 本框架不替模块决定「未登记 key 显示什么」，
	// 登记与兜底是声明方的事（模块 enums 的 Err 工厂 + 覆盖测试）。
	return out
}

// MessageOf 只算一个错误的对外文案，不写响应、不设状态码。
//
// 供「要文案但不要出口」的地方使用：整页失败槽、片段里的一行结论文案。
// 判据与 ErrorAuto **是同一个**（IsBusinessError）—— 出口各写一份判定，症状是同一条错误
// 在接口上给了具体原因、在页面上只说「服务器内部错误」，而两边都不报错。
//
// scene 用于日志定位；业务错误是预期内分支，不记日志。err 为 nil 时返回空串。
func MessageOf(c *gin.Context, scene string, err error) string {
	if err == nil {
		return ""
	}
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		return businessText(c, scene, fact)
	}
	if scene == "" {
		scene = "http"
	}
	logInternalError(c, scene, "handler 内部错误", err)
	return translate(c, msgInternalError)
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
//
// **例外：内部错误 key 不许裸奔**。InternalKey 是"系统出了故障"那条归口文案的 key，
// 它的兜底原文（InternalFallback）在本包里就有。译文取不到时若原样回 key，
// 客户端与用户看到的是 `MsgInternalError` 这个内部标识符 —— 既是界面 key 外泄，
// 也丢失了"系统内部错误"这个唯一有用的信息。实测触发路径：i18n 缓存只装了部分词条
// （例如启动后词条表短暂不可用、或测试注入），500 响应体就是裸 key。
func translate(c *gin.Context, message string) string {
	if text, ok := translateKnown(c, message); ok {
		return text
	}
	if message == InternalKey {
		return InternalFallback
	}
	return message
}

// translateKnown 与 translate 同义，但第二个返回值表示"命中译文"。
//
// 拆出来是为了让"未命中"这条路只在一处兜底：原先 translate 的每个分支都用
// `text != key` 判断命中，未命中的尾部逻辑（原样返回）无法插话，
// 于是"未命中时给什么"这件事根本没地方写。
func translateKnown(c *gin.Context, message string) (string, bool) {
	lang := requestLanguage(c)

	// 形态 1：纯 key
	if text := i18n.GetText(message, lang); text != message {
		return text, true
	}

	// 形态 2：key|param1|param2
	if idx := strings.Index(message, "|"); idx > 0 {
		key := message[:idx]
		if text := i18n.GetText(key, lang); text != key {
			// 协议限定：模板只允许 %s / %[n]s / %%（参数按字符串注入）。
			// 含 %d/%f 等协议外占位符时不做注入，按 key 原文降级，避免 Go 格式错误输出。
			if !i18n.HasStringPlaceholdersOnly(text) {
				return message, false
			}
			args := strings.Split(message[idx+1:], "|")
			anyArgs := make([]interface{}, len(args))
			for i, a := range args {
				anyArgs[i] = a
			}
			return fmt.Sprintf(text, anyArgs...), true
		}
	}

	// 形态 3：key: detail
	if idx := strings.Index(message, ": "); idx > 0 {
		prefix := message[:idx]
		if text := i18n.GetText(prefix, lang); text != prefix {
			return text + message[idx:], true
		}
	}

	return message, false
}
