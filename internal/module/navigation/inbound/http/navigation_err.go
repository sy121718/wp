package navigationhttp

// navigation_err.go — 导航 JSON 接口的错误响应归口（审计 CQ-009）。
//
// 背景：本模块 5 个接口（Create / Update / Get / List / Delete）原先都是
// `response.ErrorWithMessage(c, navigationErrorStatus(err), err.Error())`，而
// navigationErrorStatus 的 default 分支是 **500** —— service 一旦把 PostgreSQL 原文上抛
// （表名、唯一约束名、SQLSTATE），它就会随 500 原样直出给前端。响应不是可信边界。
// 门禁：scripts/check-no-internal-error-leak.sh。
//
// 与 admin 的 admin_err.go 同形：命中本模块 enums 白名单（NavigationFacingMessages）
// → 原样透出（前端要据此提示「哪一项不合法」）；未命中 → 记结构化日志（场景 + user_id
// + 原始错误）并返回 navigationenums.ErrInternal。状态码语义不变，仍由
// navigationErrorStatus 决定（业务错误 400/404、内部错误 500）。

import (
	"strings"

	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// errFacingDetailSep 业务文案与定位信息的分隔符（乐观锁冲突等带定位的场景）。
//
// 写侧（service.staleVersionMessage）固定用全角「：」拼装；这里同时认半角「: 」，
// 是为了与 shell.FacingNotice 的形态 3（两种分隔符都认）保持同一套读法 ——
// 将来写侧按语言改用半角时，读侧不必跟着改。
const errFacingDetailSep = "："

// navigationErrScene 日志场景名（与 navigation 模块其它 logger.Scene("navigation") 一致）。
const navigationErrScene = "navigation"

// navigationFacingText 命中白名单 → 返回可对外文案（可能是 key 或 `key|param` 形态，
// 参数交给翻译层填充）；未命中 → ("", false)。
//
// 带参协议与 admin 同形：文案本身是 i18n key，参数走 `key|param`，所以匹配时
// 除整串相等外还认 `key|` 前缀 —— 只放行 key 命中白名单的那些参数串。
//
// 拆出这个**不记日志**的版本，是给 HTML 页面路径用的：导航译文工作台在调用点已经记了
// 一条更具体的日志（带 lang），再走 navigationErrText 会为同一个错误记两条。
func navigationFacingText(err error) (string, bool) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	// 命中判据与白名单同源（enums.HitFacingMessage）：三种形态 ——
	// 整串相等 / key|param / key：定位信息。本函数只多负责「不命中就记日志」那一层。
	return navigationenums.HitFacingMessage(msg)
}

// localizeFacing 把白名单命中的文案转成**当前语言下可直接渲染**的一句。
//
// 三种形态（与 navigationFacingText 的判据一一对应）：
//
//   - key —— 裸 key，交给 pkg/response 的取词（它本来就是为这个出口设计的）；
//   - key|param —— 带参协议，同样交给 pkg/response（词条里的 %s 由它填）；
//   - key：<定位> —— 本模块自己拼的形态（乐观锁冲突就长这样），必须自己翻：
//     response 的 translate 不认识全角「：」，不翻的话页面上会原样出现裸 key。
//
// 页面与 JSON 两个出口共用它，因此「同一个错误在页面与接口上说法一致」不是靠两边各写一遍。
func localizeFacing(c *gin.Context, msg string) string {
	if key, detail, ok := cutFacingDetail(msg); ok {
		return shell.TranslateFor(c)(key, key) + facingDetailSep(c) + detail
	}
	return response.TranslateMessage(c, msg)
}

// cutFacingDetail 从白名单文案里拆出「key + 定位信息」；不是该形态返回 ok=false。
// 判据在 enums（与白名单同一文件、同一份读法）：service 侧的 FacingText 用的是同一个，
// 所以页面出口与 contract 出口不会对「哪一段是 key、哪一段是定位」产生分歧。
func cutFacingDetail(msg string) (key, detail string, ok bool) {
	return navigationenums.SplitFacingDetail(msg)
}

// facingDetailSep 定位信息的分隔符：中文用全角冒号，其余语言用「: 」。
//
// 读侧 shell.FacingNotice 的形态 3 两种都认，所以写读两侧不会因语言切换而失配
// （写侧固定发全角、这里按语言渲染，是因为提示条最终是给人看的一句话）。
func facingDetailSep(c *gin.Context) string {
	if strings.HasPrefix(response.RequestLanguage(c), "zh") {
		return errFacingDetailSep
	}
	return ": "
}

// navigationErrText 把 service 错误收敛为可对外展示的文案（JSON 接口）。
//
// 命中白名单 → 原样返回；未命中 → 记一条结构化日志并返回归口文案。
func navigationErrText(c *gin.Context, err error) string {
	if msg, ok := navigationFacingText(err); ok {
		return localizeFacing(c, msg)
	}

	logger.Scene(navigationErrScene).
		With("user_id", shell.CurrentUserID(c)).
		Error(err, "navigation 接口内部错误")
	return navigationenums.ErrInternal
}

// navigationErrPageText HTML 页面路径的归口文案（整页渲染 + 提示条，不走 pkg/response）。
//
// 为什么单独收口：页面把 err.Error() 拼进提示条的形态（`"保存失败：" + uerr.Error()`）
// 泄漏的是**页面正文**，而 scripts/check-no-internal-error-leak.sh 的判据只看
// response.ErrorWithMessage / c.String 的实参 —— 这种形态它覆盖不到。
// 命中白名单 → 原样（业务文案，运营要知道「哪一项不合法」）；
// 未命中 → 归口到 navigationenums.ErrInternal 并翻成当前语言，数据库原文只进日志。
func navigationErrPageText(c *gin.Context, err error) string {
	if msg, ok := navigationFacingText(err); ok {
		return localizeFacing(c, msg)
	}
	return shell.TranslateFor(c)(navigationenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）")
}

// —— 参数级提示（缺必填字段 / 未勾选等）——
//
// 页面写操作的参数校验失败此前走 `response.ErrorWithMessage(c, 400, navFieldRequiredMsg)`：
// 浏览器收到的是一段 JSON（`{"code":400,"message":"必填字段不能为空"}`），**页面完全脱离
// 页壳** —— DOM 里只有那个 JSON 节点，没有标题栏与左侧菜单，地址栏停在 POST 路径，
// 用户只能手改地址栏回列表页。参数级失败与业务失败一样是「页面请求」，出口就该是页面。
//
// 同域已做对的页面（page 的 pagesBackURL / content 的 articleRedirectList /
// block 的 siteSlotRedirect）在同样场景下一律 303 回列表页 + ?err= 回带原因，本模块照此收口；
// 回跳地址仍由 navListURLMenu（本页唯一的列表 URL 构造点）给出。

// navInvalidParamText 参数级校验失败的可展示文案（当前语言）。
//
// 参数级失败没有 error 对象（判定就在 handle 里），因此走不了 navigationErrPageText；
// 但文案必须落在 navigationNoticeTexts 的候选里 —— 读侧 navigationPageErr 按形状**整体**
// 匹配，未命中的 query 参数会被当伪造文案丢掉（页面上什么都不显示，也没有任何日志）。
// ErrInvalidParam 本来就在 enums 白名单里，key 形态与译文形态都是候选，这一层天然成立。
func navInvalidParamText(c *gin.Context) string {
	return shell.TranslateFor(c)(navigationenums.ErrInvalidParam, "参数错误")
}

// navNoticeNoSourcePicked 「从已有内容添加」一项都没勾时的提示。
//
// 这是该抽屉最常见的一条路径（候选复选框默认全不勾，用户直接点「加入菜单」）。
// 文案与 navigationsBulkResultTemplates 同形：中文常量、**写侧与读侧共用这一份字面量**，
// 读侧经 shell.NoticeTemplate 归一后整体比对（另抄一份中文的失配是静默的 —— 提示发出来了，
// 页面上却不显示）。模板侧另有「本组没有可加入项时按钮置灰」，这里是服务端兜底。
const navNoticeNoSourcePicked = "请至少勾选一项要加入菜单的内容。"

// —— 读侧回执的收口（?err= / ?done=）——
//
// 写侧早就是受控的（NavigationCreate/Update/Delete 走 response 出口；批量删除走
// shell.BulkIDsFacingText 与 navigationsBulkDeleteResult），但列表页此前把 query 参数
// **原样**塞进渲染数据（navigationsPageData 的 Err / Done）：任何人手拼一个
// /admin/navigations?err=任意文案 就能在页面上塞一条顶着「上一次操作未完成」样式的伪造消息。
// 查询参数与响应体、模板数据一样**不是可信边界**。

// navigationsBulkResultTemplates 批量删除的结论文案模板（%d 是计数字段）。
//
// **写侧与读侧共用这一份字面量**：写侧 navigationsBulkDeleteResult 用它 Sprintf，
// 读侧 navigationNoticeTexts 用它（经 shell.NoticeTemplate 归一）判定 URL 回显。
var navigationsBulkResultTemplates = []string{
	"没有勾选任何菜单项，列表未改动。",
	"已删除 %d 个菜单项。",
	"%d 个菜单项都未能删除，列表未改动。",
	"已删除 %d 个，%d 个未能删除（可能已被删除）。",
}

// navigationNoticeTexts 本页可以原样展示的回执文案（当前语言）。
//
// 与写侧的取值一一对应：① 本模块 enums 白名单的 key 与其译文（navigationErrPageText
// 的产物，key 形态与译文形态都收）；② 归口文案与 shell 的批量上限提示模板；
// ③ 批量结论文案模板。
func navigationNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0, len(navigationenums.NavigationFacingMessages)*2+8)
	for _, key := range navigationenums.NavigationFacingMessages {
		out = append(out, key, tr(key, key))
	}
	out = append(out,
		tr(navigationenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）"),
		shell.BulkIDsNoticeTemplate(c),
	)
	// 参数级提示：navInvalidParamText 的产物（ErrInvalidParam 的译文）已在上面 enums
	// 白名单里；「未勾选」是本地常量，与批量结论模板同一读法（归一后整体比对）。
	out = append(out, shell.NoticeTemplate(navNoticeNoSourcePicked))
	for _, tpl := range navigationsBulkResultTemplates {
		out = append(out, shell.NoticeTemplate(tpl))
	}
	return out
}

// navigationPageErr 列表页 ?err= 的统一出口（未命中落归口文案）。
func navigationPageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, navigationNoticeTexts(c))
	})
}

// navigationPageDone 列表页 ?done= 的统一出口（成功提示：未命中落空串）。
func navigationPageDone(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("done"), "", func(raw string) string {
		return shell.FacingNotice(raw, navigationNoticeTexts(c))
	})
}
