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
// 页面出口的两件事（判据只有一条，不再由本文件持有）：
//  ① 可透出性 —— 交给 adminui.PageErrorFact（判据是 pkg/response.IsBusinessError 的**形态**）。
//     这里原先挂着 pluginFacingMessages 白名单 + 「整串相等或 key+": " 前缀」的逐条比对：
//     那是**第二份真相** —— service 换了措辞、或某个 key 改由别的模块提供，白名单就静默失配，
//     而两种症状都不报错（有时把内部错误当业务文案透出、有时把业务文案吞成归口）；
//  ② 出口渲染 —— 结论由 shell.RenderJump 渲染成整页提示（文案走响应体，模板用注入的 tf 取词）。
//     正文是**事实**（key + 中文兜底 + `%s` 参数），不是拼好的句子 —— 控制器不取词。
//
// **读侧（?err= 的受控文案集合与判定）已整批删除**：写动作的结论不再经查询参数回带，
// 那套「证明这条提示出自本仓」的判定（pluginNoticeTexts / pluginPageErr）随之不需要了。
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
// 出口把 key 交给模板注入的 tf（迁移 058 已 seed 中英词条），常量名照样渲染成
// 「插件包解析失败」，词条缺失时还有本文件的中文兜底。

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/internal/shell"
)

const (
	// pluginErrScene 日志场景名（与 plugin 模块其它 logger.Scene("plugin") 调用点一致）。
	pluginErrScene = "plugin"
	// pluginPagePath 页面写操作的统一回跳目标（插件列表页）。
	pluginPagePath = "/admin/plugins"
)

// pluginMsg 页面出口的一句文案（成功回执 / 失败原因 / 补语）。
//
// 就是共享形状 adminui.Msg（key + 中文兜底 + `%s` 位置参数）——用别名而不是自造 struct：
// 模块各造一份同形类型就是多份契约，字段名与参数编码迟早分叉，而症状是
// 「某个页面的提示偶尔少一个数字」，既不报错也不记日志（同 page 模块的 pageMsg）。
type pluginMsg = adminui.Msg

// 页面出口可能展示给运营的业务文案（key + 词条缺失时的中文兜底）。
//
// 名称沿用迁移前的 pluginFacingMessage：key 用于查词条（迁移 058 里 ErrInstallParse 等
// key 已有 zh-CN / en-US 两行），fallback 保证**词条缺失时页面上仍然是中文**，
// 而不是把裸 key 渲染给运营 —— 这正是本轮缺陷的表象。
var (
	pluginTextInvalidParam    = pluginMsg{Key: pluginenums.ErrInvalidParam, Fallback: "参数不完整，请检查后重试"}
	pluginTextPluginNotFound  = pluginMsg{Key: pluginenums.ErrPluginNotFound, Fallback: "插件不存在"}
	pluginTextInstallParse    = pluginMsg{Key: pluginenums.ErrInstallParse, Fallback: "插件包解析失败"}
	pluginTextUnsafePackage   = pluginMsg{Key: pluginenums.ErrUnsafePackage, Fallback: "插件包包含不安全内容，已拒绝"}
	pluginTextVersionRegress  = pluginMsg{Key: pluginenums.ErrVersionRegress, Fallback: "插件版本不能回退"}
	pluginTextStorageFailure  = pluginMsg{Key: pluginenums.ErrStorageFailure, Fallback: "插件包存储失败"}
	pluginTextToggleFailed    = pluginMsg{Key: pluginenums.ErrToggleFailed, Fallback: "插件状态更新失败"}
	pluginTextUninstallFailed = pluginMsg{Key: pluginenums.ErrUninstallFailed, Fallback: "插件卸载失败"}
	pluginTextMigrationFailed = pluginMsg{Key: pluginenums.ErrMigrationFailed, Fallback: "插件数据层迁移失败"}
)

// 页面自造文案（不是 enums 常量）。
//
// 取值形态与上面的业务文案一致（key + 中文兜底，经插件出口取词）：这五句此前是裸中文
// 常量，英文站点上恒为中文 —— 中文当 key 必然查不到（库内没有中文 item_key），也没有任何
// 办法替换。登记成 key 之后与业务文案走同一条降级链（词条 → 中文兜底）。
var (
	pluginNoticeModuleUnwired = pluginMsg{Key: pluginenums.ErrModuleUnwired, Fallback: "插件模块未装配，该操作无法执行"}
	pluginNoticeNoFile        = pluginMsg{Key: pluginenums.ErrInstallNoFile, Fallback: "没有收到插件包文件"}
	pluginNoticeUnreadable    = pluginMsg{Key: pluginenums.ErrPackageUnreadable, Fallback: "插件包读取失败，或文件超过 52MB 上限"}

	// pluginNoticeListFailed 列表取数失败的提示（只进模板数据的提示条）。
	pluginNoticeListFailed = pluginMsg{Key: pluginenums.ErrListFailed, Fallback: "插件列表加载失败"}

	// pluginReselectHint 安装路径的补充提示：回跳会丢掉用户已选的文件。
	//
	// 安装是 multipart 上传（上限 52MB，见 plugin_page_handle.go 的 pluginsUploadMax），
	// 表单字段只属于那一次 POST —— 303 回列表页后浏览器不会重传，用户必须重新选文件。
	// 不说这句的话，用户看到「插件包解析失败」只会重按一次提交按钮（而那份文件早没了）。
	pluginReselectHint = pluginMsg{Key: pluginenums.NoticeInstallReselect, Fallback: "浏览器不会重传已选文件，请重新选择文件后再提交"}
)

// pluginFallbacks 本模块认识的 key → 中文兜底（**只是缺词条时的展示原文**）。
//
// 它不参与可透出性判定 —— 那条判据只有一份，在 adminui.PageErrorFact。漏登记一条的后果是
// 页面上出现一句英文常量名（可见、可查），方向是安全的；而如果反过来拿它当白名单用，
// 就退回了「第二份真相」。
var pluginFallbacks = []pluginMsg{
	pluginTextInvalidParam,
	pluginTextPluginNotFound,
	pluginTextInstallParse,
	pluginTextUnsafePackage,
	pluginTextVersionRegress,
	pluginTextStorageFailure,
	pluginTextToggleFailed,
	pluginTextUninstallFailed,
	pluginTextMigrationFailed,
	pluginNoticeModuleUnwired,
	pluginNoticeNoFile,
	pluginNoticeUnreadable,
	pluginNoticeListFailed,
	pluginReselectHint,
}

// pluginFacingFallback 已知 key 的中文兜底；不认识时返回空串（由调用方回落事实自带的兜底）。
func pluginFacingFallback(key string) string {
	key = strings.TrimSpace(key)
	for _, m := range pluginFallbacks {
		if m.Key == key {
			return m.Fallback
		}
	}
	return ""
}

// pluginInstallFailMsg 给安装路径的失败文案补上「重新选择文件」提示。
//
// 一句 key 表达不了这句（两段词条拼起来）：Key 为空时 adminui.Msg 的 Fallback 就是成品串，
// 这是共享形状明确支持的一档（见 internal/adminui/msg.go）。两段各自取当前语言，
// 所以整句仍然随请求语言变化 —— 不是把中文写死了。
func pluginInstallFailMsg(c *gin.Context, m pluginMsg) pluginMsg {
	return pluginMsg{Fallback: adminui.MsgText(c, m) + "；" + adminui.MsgText(c, pluginReselectHint)}
}

// pluginErrMsg service 错误 → 一句结论（提示页正文 / 片段槽位都从这里出）。
//
// 判定与日志都归 adminui.PageErrorFact：命中业务错误形态 → 该 key 的译文（缺词条回落本文件
// 的中文兜底）；其余 → 记结构化日志（带 user_id / 路径 / 原文）+ 归口文案。
//
// 带参协议（`key: 细节`）的**明细一律不透出**：plugin 的明细是 zip 驱动 / PostgreSQL 原文
// （plugin_install.go 的 `fmt.Errorf("%s: 缺少 manifest.json", ErrInstallParse)` 那一半），
// 与响应体一样不是可信边界，只进日志 —— 这正是 mail 的 `key: 细节` 形态在这里不能照抄的原因
// （mail 的细节是它自己生成的定位信息）。所以这里只取 fact.Key，fact.Tail 直接丢弃。
func pluginErrMsg(c *gin.Context, err error) pluginMsg {
	if err == nil {
		return pluginMsg{}
	}
	fact := adminui.PageErrorFact(c, pluginErrScene, err)
	if !fact.Business {
		// 归口事实（key = MsgInternalError + 中文兜底），不是空串 —— 提示页不能显示空白。
		return pluginMsg{Key: fact.Key, Fallback: fact.Fallback}
	}
	fallback := pluginFacingFallback(fact.Key)
	if fallback == "" {
		fallback = fact.Fallback
	}
	return pluginMsg{Key: fact.Key, Fallback: fallback}
}

// pluginErrText 一句结论的文本形态（片段槽位 / JSON 出口用：那里没有模板取词）。
func pluginErrText(c *gin.Context, err error) string {
	return adminui.MsgText(c, pluginErrMsg(c, err))
}

// pluginPageJump 页面写动作的统一出口：整页提示（对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 303 + `?err=`：那条通道要求读侧再判一次「这条提示是不是本仓给的」
// （pluginNoticeTexts 的候选集合），而查询参数不是可信边界。现在文案走响应体，
// 读侧判定（pluginPageErr / pluginNoticeTexts）随之整批删除。
//
// msg 是**事实**（key + 中文兜底 + `%s` 参数），不是拼好的句子：取词在渲染期由模板注入的
// tf 完成 —— 控制器不取词。语言是请求级的，Go 侧取词意味着每处都要再想一遍「这句属于哪个
// 语言」，漏一处的症状是「中文环境全对、英文界面上整句变中文」，不报错也不记日志。
//
// 失败不自动跳转（Seconds=0）：运营要看清楚原因，安装路径还带着「请重新选择文件」这句
// 必须读到的补充提示。成功 1 秒后自动回列表页（与 sysconfig / order 同一取舍）。
func pluginPageJump(c *gin.Context, ok bool, msg pluginMsg) {
	back := pluginPagePath
	backText := pluginBackMsg
	if ok {
		shell.RenderJump(c, shell.Jump{
			OK: true, MsgKey: msg.Key, Msg: msg.Fallback, MsgArgs: msg.Args,
			Back: back, BackTextKey: backText.Key, BackText: backText.Fallback, Seconds: 1,
		})
		return
	}
	shell.RenderJump(c, shell.Jump{
		MsgKey: msg.Key, Msg: msg.Fallback, MsgArgs: msg.Args,
		Back: back, BackTextKey: backText.Key, BackText: backText.Fallback,
	})
}

// pluginBackMsg 提示页「立即前往」链接的文字（复用列表页标题词条，不新增全站词条）。
//
// 包级变量并**不带 gin.Context**：这段文案属于哪个语言，由取词发生的时刻决定，
// 与控制器无关 —— 这正是本批的形状要求。
var pluginBackMsg = pluginMsg{Key: pluginenums.TitlePlugins, Fallback: "插件管理"}
