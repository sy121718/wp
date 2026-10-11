package adminui

// errors.go — 后台**页面侧**的错误出口与受控文本 helper（task-92 批 2b-1 自 internal/shell 迁入；
// task-114 收口最后两个：`PageError` → 本文件的 PageErrorInternal、`PageInternalText` 转发删除）。
//
// 为什么归这里：这些入口的消费者全是后台页面 handler（整页失败、片段写失败、参数校验失败），
// 与「外壳装配 / 页面表现」同属一层；留在 internal/shell 会让壳层长期背着页面出口，而
// internal/shell 的终局是被整体删除（迁移计划见 internal/shell/shell_migration_test.go）。
//
// 与 JSON 侧的分工（不要混）：
//   - 本文件是**页面侧**：对外只给 MsgInternalError 的当前语言译文，详情带场景进日志；
//   - **JSON API 的出口在 pkg/response**（ErrorInternal / ErrorAuto / ParamError / NotFound），
//     那边自带场景日志与「业务错误 / 内部错误」判别。JSON handler 默认不要 import 本文件。
//
// 唯一例外（两条，都是**刻意的**，别当成漏网之鱼去「顺手统一」）：
//   - `PageErrorInternal`：带壳页面的**片段端点**的 500 JSON 出口（task-114 自 shell.PageError
//     原样迁入，响应体逐字节不变）；
//   - `workbench_handle.go:64` 的 503 直接调 `PageInternalText`。
// 这两处都不能换成 pkg/response 的入口：那边不经过模板取词层，缺词条时会把**裸 key**
// （"MsgInternalError"）写进 JSON 让前端弹出来 —— 而「译文 / 中文兜底」正是本文件要挡住的事。
//
// 取词只走一处：本包的 translateFor(c)（= templates.TranslateFunc(response.RequestLanguage(c))），
// 与 Prepare 注入给模板的 t 同源 —— 页面出口的文案与模板取词不会各说各话。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// MsgInternalError 是页面级内部错误提示的 i18n key。
//
// task-114 之后本包是它**唯一**的 Go 侧定义：壳层那份副本（连同 internal/shell/
// errors_parity_test.go 的两侧漂移守卫）随调用点归零一起删掉了，不再有两份真值可漂移。
// i18n key 是字符串协议 —— 模板、词条表与各模块自己的同名常量都按字面量取值，
// TestMsgInternalErrorIsTheKeyLiteral 钉住这个字面量本身；词条本体见
// public/migrations/058_i18n_seed_enums.sql（en-US 由 407 补齐）。
const MsgInternalError = "MsgInternalError"

// PageErrorBadRequest 参数错误出口：同样不直出原文（参数错误也可能带出内部结构），
// 但状态码用 400，前端脚本能据此区分「你填错了」与「系统出问题了」。
func PageErrorBadRequest(c *gin.Context, scene string, err error) {
	if err != nil {
		logger.Scene(scene).With("path", c.Request.URL.Path).Error(err, "后台页面参数校验失败")
	}
	response.ErrorWithMessage(c, http.StatusBadRequest, MsgInternalError)
}

// PageErrorInternal 页面（片段）端点的内部错误出口：记日志 + 对外通用提示（HTTP 500，JSON 体）。
//
// 与 PageErrorBadRequest 是一对（500 / 400），取舍一致：**原文只进日志**。它是 task-114 从
// internal/shell 的 `PageError` 原样迁入的 —— 状态码、Content-Type、body 形状与日志形态
// （scene + `path` 字段 + 固定 message「后台页面处理失败」）逐字节不变：消费者是 coupon 片段
// 端点（`internal/module/order/inbound/http/coupon_page.go` 两处，浏览器拿不到页壳、只收 JSON），
// 搬家不该顺手换协议。
//
// 为什么带 Internal 后缀：本包已有 PageErrorBadRequest，两者同族同形，名字即档位。旧名
// `shell.PageError` 不带档位，容易被读成「渲染整页」—— 它从来不渲染，只写 JSON。
//
// 什么都不做地传 nil err 也**必须**写出 500：调用方常常是「拿不到细节」的失败分支，
// 一个不存在的错误不该让出口退化成 200。
func PageErrorInternal(c *gin.Context, scene string, err error) {
	if err != nil {
		logger.Scene(scene).With("path", c.Request.URL.Path).Error(err, "后台页面处理失败")
	}
	response.ErrorWithMessage(c, http.StatusInternalServerError, MsgInternalError)
}

// AdminWriteFailedText 页面写操作失败的统一响应（**文案由调用方负责**）。
//
// 契约是 —— text 必须由**拥有该业务文案的那个模块**先过一遍自己的取词之后再传进来；
// 取什么词的判据已收敛到本包的 PageErrorFact / PageErrorText（pkg/response.IsBusinessError），
// 模块不再维护 XxxFacingMessages 白名单（第二份真相，见 page_error.go 的文件头）。
//
// 为什么本层不做这个判断：本入口收到的是**已经取好词的成品串**，它无法知道调用方是想给
// 业务文案还是归口文案；在这里再猜一次「这是不是业务错误」猜错的代价是「有时透出有时吞掉」。
// 所以本入口只做两件事：trim 后为空则回落 MsgInternalError（页面不能显示空串），否则原样写出 400。
func AdminWriteFailedText(c *gin.Context, text string) {
	if c == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = MsgInternalError
	}
	response.ErrorWithMessage(c, http.StatusBadRequest, text)
}

// PageInternalText 统一内部错误文案（走当前语言的译文，缺词条回退中文原文）。
//
// 它是**受控文本 helper**，不是出口：只产文案，写不写响应、写什么状态码由调用方决定
// （整页 500、片段 500、跳转提示、JSON 体都用它）。
//
// 中文兜底与 058 的 zh-CN 词条**逐字相同**，所以「命中词条」与「回落兜底」返回同一个
// 字符串；只有兜底被改措辞、或词条被改成别的说法时才会不等 —— 那正是该被测试拦下的漂移。
func PageInternalText(c *gin.Context) string {
	return translateFor(c)(MsgInternalError, internalErrorFallback)
}

// internalErrorFallback 归口文案的中文原文（与 058 的 zh-CN 词条逐字相同）。
//
// 单独取名是因为它有两个消费者：本文件的 PageInternalText，与 page_error.go 里
// 「未命中业务错误判据」时构造的 ErrFact —— 两处必须是同一句话，抄第二遍必然漂移。
const internalErrorFallback = "系统内部错误，请稍后重试"
