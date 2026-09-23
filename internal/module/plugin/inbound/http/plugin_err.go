package pluginhttp

// plugin_err.go — 插件管理页 handler 的错误文案归口（对齐 AGENTS.md「响应与错误处理」）。
//
// 为什么需要这一层：本模块的页面 handler 此前一律以 `c.String(4xx, pluginenums.ErrXxx)` 收场，
// 三个坏处同时出现：
//   · 响应体是 i18n key 本身（`ErrInstallParse` / `ErrToggleFailed` / `ErrUninstallFailed`），
//     而且**脱离页壳**：浏览器停在 POST 路径上，用户看到的是一行 15~18 字节的英文标识符，
//     没有导航、没有返回、也不知道下一步做什么；
//   · 直出违反 AGENTS.md「后台页面 handler 禁止 c.String 直出内部错误」的形态①；
//   · 安装是 multipart 上传，一旦不回列表页，用户已选的文件就白选了（浏览器不会重传）。
//
// 页面出口（303 + ?err=）的三件套，样板见 admin 的 admin_err.go 与 mail 的 mail_err.go：
//  ① 白名单 —— 只有本模块 enums 声明的业务文案才允许透出，且**经翻译层取词**；
//  ② 归口文案 —— 未命中（PostgreSQL 原文 / zip 驱动原文 / 路径）只进日志，页面给统一提示；
//  ③ 读侧收敛 —— ?err= 是查询参数，不是可信边界，必须整体命中受控文案集合才渲染。
//
// **为什么 enums 的常量值不动（plugin_enums.go 保持 key 形态）**：
// 那些常量同时是 JSON 接口（/api/plugin/*）的响应消息，而 `pkg/response.IsBusinessError`
// 的判据是**形态**（`ErrXxx` 常量名，或 `模块.err.语义` 的 key），且注释里写明「纯中文短文案」
// 那一层已删除（pkg/response/error_auto_test.go 有用例 `{"两个中文段", errors.New("页面不存在")}`
// 断言它必须判为**非**业务错误）。把值改成中文会让 plugin 的 14 个常量两层判据都不命中：
// `plugin_handle.go` 的两处 `response.ErrorAuto` 立刻把「插件包解析失败」这类业务失败
// 从「400 + 业务文案」降级成「500 + 服务器内部错误，请稍后重试」，并让
// `TestIsBusinessErrorCoversAllModuleEnums` 的逐常量对账变红。
// 而页面侧要的是「用户看到中文」—— 这条链路由**出口**决定，不由常量值决定：
// 出口经 pluginFacingText 走翻译层（迁移 058 已 seed 中英词条），常量名照样渲染成
// 「插件包解析失败」，词条缺失时还有本文件的中文兜底。

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
)

const (
	// pluginErrScene 日志场景名（与 plugin 模块其它 logger.Scene("plugin") 调用点一致）。
	pluginErrScene = "plugin"
	// pluginPagePath 页面写操作的统一回跳目标（插件列表页）。
	pluginPagePath = "/admin/plugins"
)

// pluginFacingMessage 一条可对外透出的业务文案：i18n key + 词条缺失时的中文兜底。
//
// 与 admin 的 adminBulkText 同型（key + fallback 成对）：key 用于查词条（迁移 058 里
// ErrInstallParse 等 key 已有 zh-CN / en-US 两行），fallback 保证**词条缺失时页面上
// 仍然是中文**，而不是把裸 key 渲染给运营 —— 这正是本轮缺陷的表象。
type pluginFacingMessage struct{ key, fallback string }

var (
	// 页面出口可能接住的全部业务文案（= pluginenums 里的 Err* 常量，逐个对应）。
	// Msg* 是成功回执，页面路径的成功分支只回 303 不带 ?ok=，所以不在这里。
	pluginTextInvalidParam    = pluginFacingMessage{pluginenums.ErrInvalidParam, "参数不完整，请检查后重试"}
	pluginTextPluginNotFound  = pluginFacingMessage{pluginenums.ErrPluginNotFound, "插件不存在"}
	pluginTextInstallParse    = pluginFacingMessage{pluginenums.ErrInstallParse, "插件包解析失败"}
	pluginTextUnsafePackage   = pluginFacingMessage{pluginenums.ErrUnsafePackage, "插件包包含不安全内容，已拒绝"}
	pluginTextVersionRegress  = pluginFacingMessage{pluginenums.ErrVersionRegress, "插件版本不能回退"}
	pluginTextStorageFailure  = pluginFacingMessage{pluginenums.ErrStorageFailure, "插件包存储失败"}
	pluginTextToggleFailed    = pluginFacingMessage{pluginenums.ErrToggleFailed, "插件状态更新失败"}
	pluginTextUninstallFailed = pluginFacingMessage{pluginenums.ErrUninstallFailed, "插件卸载失败"}
	pluginTextMigrationFailed = pluginFacingMessage{pluginenums.ErrMigrationFailed, "插件数据层迁移失败"}
)

// pluginFacingMessages 白名单本体。
//
// 方向是安全的：漏写一条只会让页面显示一句通用提示（可见、可查），而如果反过来用
// 「黑名单 + 直出 err.Error()」，service 一旦上抛 PostgreSQL 原文（表名 / 约束名 / SQLSTATE）
// 或 zip 驱动原文，它就会随 303 的 Location 摆到运营面前 —— 响应不是可信边界。
var pluginFacingMessages = []pluginFacingMessage{
	pluginTextInvalidParam,
	pluginTextPluginNotFound,
	pluginTextInstallParse,
	pluginTextUnsafePackage,
	pluginTextVersionRegress,
	pluginTextStorageFailure,
	pluginTextToggleFailed,
	pluginTextUninstallFailed,
	pluginTextMigrationFailed,
}

// 页面自造文案（不是 enums 常量，但同样会进 ?err=）。
//
// 受控性来自「整句都由本页拼出 + 逐条登记在 pluginNoticeTexts 里」，
// 与 mailFormErrText / shell.BulkIDsFacingText 同一判据：**看文案来自哪里**。
const (
	pluginNoticeModuleUnwired = "插件模块未装配，该操作无法执行"
	pluginNoticeNoFile        = "没有收到插件包文件"
	pluginNoticeUnreadable    = "插件包读取失败，或文件超过 52MB 上限"

	// pluginNoticeListFailed 列表取数失败的提示（只进模板数据，不经 ?err= 回带，所以不在
	// pluginNoticeTexts 的候选里：它不是查询参数的产物，放进候选只会给手拼 URL 多一个可伪造的句子）。
	pluginNoticeListFailed = "插件列表加载失败"

	// pluginReselectHint 安装路径的补充提示：回跳会丢掉用户已选的文件。
	//
	// 安装是 multipart 上传（上限 52MB，见 plugin_page_handle.go 的 pluginsUploadMax），
	// 表单字段只属于那一次 POST —— 303 回列表页后浏览器不会重传，用户必须重新选文件。
	// 不说这句的话，用户看到「插件包解析失败」只会重按一次提交按钮（而那份文件早没了）。
	pluginReselectHint = "浏览器不会重传已选文件，请重新选择文件后再提交"
)

// pluginInstallFailText 给安装路径的失败文案补上「重新选择文件」提示。
//
// 写侧（pluginPageFail 的调用点）与读侧（pluginNoticeTexts 的候选）**共用这一个函数**：
// 读侧另拼一份的话，写侧文案一改，?err= 就会静默失配，表现是「失败但页面上没有任何提示」。
func pluginInstallFailText(text string) string {
	return text + "；" + pluginReselectHint
}

// pluginFacingText 取一条业务文案的当前语言文本（词条缺失回落中文兜底）。
func pluginFacingText(c *gin.Context, m pluginFacingMessage) string {
	return shell.TranslateFor(c)(m.key, m.fallback)
}

// pluginErrParam service 错误的**页面路径**归口：返回一句可直接渲染的文案。
//
// 命中白名单 → 该条文案的当前语言译文；未命中 → 记结构化日志（带 user_id / 路径 / 原文），
// 返回 shell.PageInternalText(c) 的统一提示。
//
// 带参协议：service 用 `key + ": " + 细节` 承载定位信息（plugin_install.go 的
// `fmt.Errorf("%s: 缺少 manifest.json", ErrInstallParse)`），所以匹配时除整串相等外
// 还认 `key + ": "` 前缀；但**细节一律不透出** —— 那一半是 zip / PostgreSQL 驱动原文，
// 与响应体一样不是可信边界，只进日志（这正是 mail 的 `key: 细节` 形态在这里不能照抄的原因：
// mail 的细节是它自己生成的定位信息，plugin 的细节是第三方原文）。
func pluginErrParam(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	for _, m := range pluginFacingMessages {
		if msg == m.key || strings.HasPrefix(msg, m.key+": ") {
			return pluginFacingText(c, m)
		}
	}

	logger.Scene(pluginErrScene).
		With("user_id", shell.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "插件后台页处理失败")
	return shell.PageInternalText(c)
}

// pluginNoticeTexts 页面 ?err= **可以原样渲染**的受控文案集合（当前语言）。
//
// 它是写侧全部出口的镜像：改了写侧文案就要在这里同步 —— 漏了不会报错，
// 只会让运营看到「提交失败但页面上什么都没说」（读侧未命中落空串，见 pluginPageErr）。
// 候选同时覆盖「基础文案」与「基础文案 + 重选提示」两种形态（安装路径会产出后者）。
func pluginNoticeTexts(c *gin.Context) []string {
	base := make([]string, 0, len(pluginFacingMessages)+3)
	for _, m := range pluginFacingMessages {
		base = append(base, pluginFacingText(c, m))
	}
	base = append(base,
		pluginNoticeModuleUnwired,
		pluginNoticeNoFile,
		pluginNoticeUnreadable,
		shell.PageInternalText(c),
	)

	out := make([]string, 0, len(base)*2)
	out = append(out, base...)
	for _, text := range base {
		out = append(out, pluginInstallFailText(text))
	}
	return out
}

// pluginPageErr 页面 ?err= 的统一出口：形状清洗 + 整体命中受控文案，未命中落归口文案。
//
// 未命中落归口文案（而不是像 ?done= 那样落空串）：这是**错误**通道，写侧每次失败都会
// 给出一句话，「必须说点什么」由写侧保证；真出现未命中也说明运营遇到了失败，
// 给一句通用提示比什么都不说更接近真实情况。
func pluginPageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, pluginNoticeTexts(c))
	})
}

// pluginPageFail 页面写失败的统一出口：303 回插件列表页，原因经 ?err= 受控文案回带。
//
// 为什么是 303 而不是 302：POST 之后必须换成 GET 才回页面（否则刷新会重发表单），
// 与同文件成功路径（c.Redirect(http.StatusSeeOther, …)）同一取舍。
//
// 回带的值必须**已经**是受控文案（pluginFacingText 的译文 / 本文件登记的常量），
// 不接受 err.Error() —— 门禁 scripts/check-no-internal-error-leak.sh 的形态②正是这一条。
func pluginPageFail(c *gin.Context, text string) {
	c.Redirect(http.StatusSeeOther, pluginPagePath+"?err="+url.QueryEscape(text))
}
