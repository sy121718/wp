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
//
// 本文件同时承载**页面写路径**的文案归口（navigationErrMsg / navInvalidParamMsg）
// 与提示页要用的写侧文案（navText）：页面与接口共用同一份白名单，
// 「同一个错误在接口与页面上说法一致」因此不是靠两边各写一遍。
//
// 两个出口的产物形态**不同，这是有意的**（本批按审计口径收敛）：
//   · JSON 接口（本文件的 navigationErrText）只做白名单收敛 + 协议形态对齐，交给
//     response.ErrorWithMessage → pkg/response 按请求语言翻译 —— 单一取词入口；
//   · 页面出口（navigationErrMsg / navInvalidParamMsg）产出的是**事实**
//     （adminui.Msg：key + 中文兜底 + `%s` 参数），取词发生在渲染期
//     （提示页注入的 tf）。控制器不取词：语言是请求级的，Go 侧提前翻成整句意味着
//     每处都要再判一次「这句属于哪个语言」，漏一处就是「中文环境全对、英文界面整句中文」。
// 此前两边都经 localizeFacing 取词（页面那条走 shell.TranslateFor），JSON 那条等于
// 「翻两遍 + 自带第二套读法」。
//
// **读侧回执（?err= / ?done= 的受控文案集合与判定）已随本批整批删除**：写动作的结论
// 改由 shell.RenderJump 渲染整页提示（文案走响应体，见 navigation_jump.go），
// 那套「证明这条提示出自本仓」的判定（navigationNoticeTexts / navigationPageErr /
// navigationPageDone）不再需要。

import (
	"go_wp/internal/middleware/builtin"
	"strings"

	"go_wp/internal/adminui"
	navigationenums "go_wp/internal/module/navigation/enums"
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

// navigationErrPageFallback 缺词时的最后一句中文（页面整页提示，以及 JSON 批量的逐条原因）。
//
// 两个「不能」：页面不能显示空串（错误槽空着，用户以为没出错），也不能显示裸 key
// （页面上出现一串英文常量）。JSON 的**顶层 message** 不受这条约束 —— 它的协议允许回落原文、
// 由前端判；但批量的 items[].message 是列表里显示给人看的「原因」，与页面同属展示面
// （见下面 navigationReasonText / navigationReasonFallbackText）。
const navigationErrPageFallback = "操作失败，请稍后重试（细节只进日志）"

// navigationFacingFallbacks 白名单业务文案的缺词中文兜底（key → 中文原文）。
//
// 为什么展示侧要自带一份，而不复用 JSON 出口那条链：
//   - JSON 出口把 key 交给 pkg/response，它的 translate 在词条缺席时**回落 key 原文**
//     （pkg/response/response.go:321 的 `return message`）—— 顶层 message 的调用方拿到裸 key 还能自己判；
//   - 展示侧的产物是**要显示给人看的一句话**（页面提示条 / 整页提示 / JSON 批量的逐条原因）：
//     裸 key 就是「界面上出现一串英文常量」，与本文件头记的 `c.String(500, adminui.MsgInternalError)`
//     属同一症状（归口 key 未经翻译直出）。
//
// 所以展示侧单独给兜底，JSON 顶层 message 一行不动 —— 两条链各有各的缺词行为，且都是明确的：
// 词条在场 → 当前语言译文；词条缺席 → 这里的中文原文。JSON 的逐条原因（删除与来源批量加入
// 两个动作）走 navigationReasonText，用的就是本表（界面文案类的原因见 navigationReasonFallbacks）。
//
// 取值与 sys_i18n 的 zh-CN 词条**逐字一致**（迁移 058 / 290 / 403 / 429 / 602，含同 key 被别的
// 模块先登记的那几条）—— 兜底只在缺词时可见，与词条不一致会让人以为「词条坏了」。
var navigationFacingFallbacks = map[string]string{
	navigationenums.ErrInvalidParam:    "请求参数无效",
	navigationenums.ErrNotFound:        "请求资源不存在",
	navigationenums.ErrInvalidKind:     "不支持的页面类型",
	navigationenums.ErrPathTaken:       "同工程同类型下已存在相同路径的导航项",
	navigationenums.ErrInvalidSource:   "非法的菜单项来源，仅支持 custom/page/article/product/category/block，且非 custom 必须指定来源实体",
	navigationenums.ErrInvalidTarget:   "非法的打开方式，仅支持 self 或 blank",
	navigationenums.ErrInvalidParent:   "父级菜单不合法：父项必须存在、与本项属于同一工程与同一类型，且不能形成循环",
	navigationenums.ErrProjectRequired: "请先选择要操作的站点工程",
	navigationenums.ErrStaleVersion:    "该菜单项已被其他窗口修改，请刷新后重试",
	navigationenums.ErrSortTied:        "同一级里存在重复的排序值，本次没有产生位移；请先把这一级的顺序理顺",
}

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

// localizeFacing 把白名单命中的文案转成一条**事实**（**页面出口专用**）。
//
// 三种形态（与 navigationFacingText 的判据一一对应）：
//
//   - key —— Msg{Key, Fallback}：取词留在渲染期（提示页的 tf / 模板的 tr），
//     key 非空时词条缺席才回落 Fallback，所以缺词也不会把裸 key 渲染到页面上；
//   - key|param —— 同上并带 `%s` 参数（参数注入的实现是 adminui.TranslateArgs，
//     模板的 tf 与它是同一份）；
//   - key：<定位> —— 一段 key 表达不了「译文 + 分隔符 + 定位」这三段（与 page 的
//     pageErrMsgOf 同一取舍），所以这一档给成品串（Key 留空，Fallback 就是正文）。
//     定位信息是任意文本、分隔符还要按语言取，收成词条属于独立的文案迁移。
//
// 页面（提示条 / 整页提示）与 JSON 接口的产物**刻意不同**：JSON 出口把 key 原样交给
// pkg/response（见 navigationErrJSONMessage），页面这条给的是渲染期可消费的事实。
// 两处共用同一份白名单与同一个拆分判据（都在 enums），所以「同一个错误在页面与接口上
// 说法一致」不靠两边各写一遍。
func localizeFacing(c *gin.Context, msg string) adminui.Msg {
	// 页面侧的缺词兜底：词条缺席时不显示裸 key（见 navigationFacingFallbacks 的说明）。
	// 局部闭包而非包级 helper —— 它不写响应，只是把「这一档说什么」集中一处。
	fallbackFor := func(key string) string {
		if text := navigationFacingFallbacks[key]; text != "" {
			return text
		}
		// 白名单新增了 key 却忘了登记兜底：退回归口事实（比空串与裸 key 都好）。
		return adminui.InternalMsg().Fallback
	}

	if key, detail, ok := cutFacingDetail(msg); ok {
		// 复合句：这里必须现在取词（Key 为空表示 Fallback 就是成品串，渲染期原样输出）。
		return adminui.Msg{
			Fallback: adminui.TranslateArgs(c, key, fallbackFor(key), nil) + facingDetailSep(c) + detail,
		}
	}

	if idx := strings.Index(msg, "|"); idx > 0 {
		return adminui.Msg{Key: msg[:idx], Fallback: fallbackFor(msg[:idx])}.WithArgs(msg[idx+1:])
	}
	return adminui.Msg{Key: msg, Fallback: fallbackFor(msg)}
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

// navigationErrText 把 service 错误收敛为 JSON 接口要发的文案（**未翻译**）。
//
// 归口只有一处：命中白名单 → 返回白名单原串（保留 enums 的 key 形态），由
// response.ErrorWithMessage → pkg/response 的取词层按请求语言翻译；未命中 → 记一条
// 结构化日志并返回 navigationenums.ErrInternal（也是 key，同样由它翻）。
//
// 为什么本模块不再自己取词（本函数此前经 localizeFacing 走 shell.TranslateFor）：
//
//	· 翻两遍 —— 本模块翻完再交给 response，后者对着一句已经翻好的中文再查一次表
//	  （必然不命中、原样返回）。不算错，但「谁负责翻译」这件事就有了两个答案；
//	· 协议分叉 —— pkg/response 认识 key / key|param / key: detail 三种形态，
//	  本模块若自己实现一遍，两边的读法迟早不一致（`：` 与 `: ` 就是现成的一例）；
//	· 测试无从下手 —— 两个取词入口都用同一份语言协商（response.RequestLanguage），
//	  输出看起来一模一样，只有把 key 交给 response 才谈得上「响应语言由它决定」。
//
// 参数 c 仍要用：内部错误那条日志要带 user_id。
func navigationErrText(c *gin.Context, err error) string {
	if msg, ok := navigationFacingText(err); ok {
		return navigationErrJSONMessage(msg)
	}

	logger.Scene(navigationErrScene).
		With("user_id", builtin.CurrentUserID(c)).
		Error(err, "navigation 接口内部错误")
	return navigationenums.ErrInternal
}

// navigationErrJSONMessage 把白名单原串对齐 pkg/response 的翻译协议（只换分隔符，不取词）。
//
// 只有「key：<定位>」这一形态需要对齐：写侧拼的是全角「：」（enums.FacingDetailSep），
// 而 pkg/response 的 translate 形态 3 只认半角 ": "。原样交出会三种形态全不命中，
// 前端拿到的就是**裸 key + 定位**（迁移 290 的注释记过这个形态：「比通用提示更糟」）。
//
// 换成品是 `key: detail` —— translate 翻前缀、保留后段，于是 JSON 里是
// 「<当前语言的文案>: <定位>」，与全仓其它模块的参数化消息同一形态。
// 页面出口刻意不跟着换：那里是给人看的一句话，中文仍用全角（见 facingDetailSep）。
func navigationErrJSONMessage(msg string) string {
	key, detail, ok := cutFacingDetail(msg)
	if !ok {
		return msg
	}
	return key + ": " + detail
}

// navigationReasonFallbacks 不在业务错误白名单里、但会出现在**逐条原因**里的界面文案兜底。
//
// 为什么另起一张表：这类 key 语义上不是「失败」（例：`admin.navigations.no_public_path`
// 「（暂无公开路径，先发布后再添加）」是一条**跳过原因**），塞进 navigationFacingFallbacks
// 会让那张表同时承担两种语义（它守的是「业务错误文案的缺词回退」）。但缺词时的回落要求一样：
// 取值与 sys_i18n 的 zh-CN 词条**逐字一致**（当前唯一一条见迁移 192）。
var navigationReasonFallbacks = map[string]string{
	navigationAddSourceNoPathKey: "（暂无公开路径，先发布后再添加）",
}

// navigationReasonFallback 逐条原因的兜底取值顺序：业务错误白名单 → 界面文案兜底 → 空串
// （空串表示两张表都没有这个 key，交给 navigationErrPageFallback 的通用最后一句）。
func navigationReasonFallback(key string) string {
	if text := navigationFacingFallbacks[key]; text != "" {
		return text
	}
	return navigationReasonFallbacks[key]
}

// navigationReasonText 把一条错误事实转成**逐条原因**（JSON 回执的 items[].message）要显示的成品句。
//
// 为什么归口到一处：删除端点（navigation_delete_write.go）与来源批量加入
// （navigation_add_source_write.go）的逐条原因都要「按请求语言出成品句 + 缺词不裸 key」，
// 两份实现等于让同一件事有两个版本。
//
// 三层，只有三层：
//  1. 词条在场 → 成品句。取词入口是 pkg/response → pkg/i18n（与 JSON 出口的其它响应同一个），
//     本模块不自己拼文案，也不新增 shell.TranslateFor 调用 —— 那是页面出口的取词面；
//  2. 词条缺席（迁移未跑 / 运营删了词条 / 语言不支持）→ 受控中文兜底；
//  3. 两张兜底表都没有的 key（ErrInternal 就是其中之一：它不在业务错误白名单里，
//     是未命中时的归口）→ navigationErrPageFallback 那一句通用最后兜底。
//
// 第 2/3 层是必须的：pkg/response 的 translate 在词条缺席时**回落传入的 key 原文**
// （pkg/response/response.go:321）。那对顶层 message 是可接受的协议（调用方能自己判），
// 但逐条原因是显示在列表行里的成品句：裸 key 与数据库原文同属「响应不是可信边界」这一类症状。
func navigationReasonText(c *gin.Context, err error) string {
	// 服务事实：白名单业务文案（key / key|param / key: detail，后者已由 navigationErrJSONMessage
	// 对齐成 JSON 出口的协议形态），未命中已归口为 ErrInternal 并记了日志。
	return navigationReasonKeyText(c, navigationErrText(c, err))
}

// navigationReasonKeyText 同上，但入口是**已经归口好的文案**（key / key|param / key: detail）。
//
// 为什么需要这一档：有些逐条原因压根没有 error 对象 —— 判定就发生在 handle 里
// （例：候选不属于本工程的该来源类型 → ErrNotFound；候选暂无公开路径 → 界面文案 key）。
// 这类原因同样不能裸着回给前端。
func navigationReasonKeyText(c *gin.Context, msg string) string {
	if text := response.TranslateMessage(c, msg); text != msg {
		return text
	}
	return navigationReasonFallbackText(msg)
}

// navigationReasonFallbackText 词条缺席时的中文兜底（形态对齐 navigationErrJSONMessage：
// 定位信息用半角 ": " 拼回，与 JSON 出口的其它消息一致）。
func navigationReasonFallbackText(msg string) string {
	key, detail := msg, ""
	if k, d, ok := cutFacingDetail(msg); ok {
		key, detail = k, d
	} else if k, _, ok := strings.Cut(msg, "|"); ok {
		// 带参形态：兜底句里没有占位符，参数（定位用的值）没有位置可放，只保留 key 的兜底句。
		key = k
	}
	if text := navigationReasonFallback(key); text != "" {
		if detail == "" {
			return text
		}
		return text + ": " + detail
	}
	return navigationErrPageFallback
}

// navigationErrMsg HTML 页面路径的错误**事实**（整页渲染 + 提示条，不走 pkg/response）。
//
// 为什么单独收口：页面把 err.Error() 拼进提示条的形态（`"保存失败：" + uerr.Error()`）
// 泄漏的是**页面正文**，而 scripts/check-no-internal-error-leak.sh 的判据只看
// response.ErrorWithMessage / c.String 的实参 —— 这种形态它覆盖不到。
// 命中白名单 → 事实（业务文案，运营要知道「哪一项不合法」）；
// 未命中 → 归口事实（本模块自己的 navigationenums.ErrInternal + navigationErrPageFallback），
// 数据库原文只进日志。
//
// 为什么归口用**模块自己**的 key 而不是共享的 adminui.InternalMsg：那一句「操作失败，
// 请稍后重试（细节只进日志）」与同文件 JSON 出口的 navigationReasonFallbackText 同源 ——
// 同一个模块对同一种失败说两套话，运营在页面与接口上看到的原因不同、而两边都不报错。
//
// 需要文本时由调用点取：adminui.MsgText(c, navigationErrMsg(c, err))；需要事实时（提示页）
// 直接交给 navJump 一族。
func navigationErrMsg(c *gin.Context, err error) adminui.Msg {
	if msg, ok := navigationFacingText(err); ok {
		return localizeFacing(c, msg)
	}
	return adminui.Msg{Key: navigationenums.ErrInternal, Fallback: navigationErrPageFallback}
}

// —— 参数级提示（缺必填字段 / 未勾选等）——
//
// 页面写操作的参数校验失败此前走 `response.ErrorWithMessage(c, 400, navFieldRequiredMsg)`：
// 浏览器收到的是一段 JSON（`{"code":400,"message":"必填字段不能为空"}`），**页面完全脱离
// 页壳** —— DOM 里只有那个 JSON 节点，没有标题栏与左侧菜单，地址栏停在 POST 路径，
// 用户只能手改地址栏回列表页。参数级失败与业务失败一样是「页面请求」，出口就该是页面。
//
// 同域页面（page 的 pagesBackURL / content 的 articleRedirectList / block 的 siteSlotRedirect）
// 同样把参数级失败当页面请求处理，本模块的出口是**整页提示**（shell.RenderJump）：
// 文案走响应体，回跳地址由 shell.BackPath 从表单 action 的 query 读回（见 navigation_jump.go）。

// navTr Go 侧取词函数（本模块**拼装标签**用的那一档；页面出口的取词一律发生在渲染期）。
//
// 为什么还需要它：有两处文案在渲染期之外就要成品串 ——
//
//	· panelBlockName：新建面板块的默认名**会落库**，必须按当前请求语言拼好
//	  （见该函数的注释：彻底的解法是给 Block 表加「默认名 key」字段，超出本批范围）；
//	· build：译文工作台的分组标题与错误槽由 Go 侧组装（尚未取词化的槽位，见报告）。
//
// 取词入口是共享设施（adminui.TranslateArgs），不再是 shell.TranslateFor —— shell 那条
// 是正在退役的旧入口，模块不该继续往它上面加调用点。
func navTr(c *gin.Context) func(key, fallback string) string {
	return func(key, fallback string) string { return adminui.TranslateArgs(c, key, fallback, nil) }
}

// navNoticeItemMissing 编辑抽屉 / 归属核对查不到这一项时的结论（key + 中文兜底）。
//
// 与 service 的 ErrNotFound 是同一个词条，但**兜底不同**：service 那条的兜底是
// 「请求资源不存在」（navigationFacingFallbacks 的登记值），而这里要说清是**菜单项**
// 不见了 —— 编辑抽屉里冒出「请求资源不存在」时，用户不知道说的是哪一层。
var navNoticeItemMissing = navText{Key: navigationenums.ErrNotFound, Fallback: "菜单项不存在"}

// navInvalidParamMsg 参数级校验失败的**事实**（key + 中文兜底，静态、不需要 gin.Context）。
//
// 参数级失败没有 error 对象（判定就在 handle 里），因此走不了 navigationErrMsg；
// 事实直接进提示页响应体（不再是 ?err= 的候选集合 —— 读侧判定已随本批删除）。
// ErrInvalidParam 在 enums 白名单里，取词在渲染期发生。
var navInvalidParamMsg = navText{Key: navigationenums.ErrInvalidParam, Fallback: "参数错误"}

// navNoticeNoSourcePicked 「从已有内容添加」一项都没勾时的提示。
//
// 这是该抽屉最常见的一条路径（候选复选框默认全不勾，用户直接点「加入菜单」）。
// 形态与 navInvalidParamMsg 同形：key + 中文兜底的事实，取词在渲染期。
// 模板侧另有「本组没有可加入项时按钮置灰」，这里是服务端兜底。
var navNoticeNoSourcePicked = navText{
	Key: navigationenums.NoticeNoSourcePicked, Fallback: "请至少勾选一项要加入菜单的内容。",
}

// —— 写侧文案（提示页用）——
//
// 读侧回执（?err= / ?done=）已随本批**整批删除**：写动作的结论由 shell.RenderJump 渲染成
// 整页提示（文案走响应体，见 navigation_jump.go），列表页不再从 query 读任何结论。
// 此前列表页把 query 参数原样塞进渲染数据（navigationsPageData 的 Err / Done），
// 任何人手拼 /admin/navigations?err=任意文案 就能塞一条伪造消息 —— 查询参数不是可信边界。
// 这里只保留写侧要用的文案类型与「加入菜单」等受控提示；
// 批量删除的结论文案已随写入口迁到 JSON（页面按 data-msg-* 取同一批 admin.navigations.bulkResult.* 词条）。

// navText 一条待取词文案的事实 —— 就是共享形状 adminui.Msg
// （key + 中文兜底 + `%s` 位置参数）。
//
// 本包所有由 Go 侧产出、会显示给用户的**结论/提示**文案都走它 —— 文案与 key 形影不离，
// 不会出现「改了文案忘了改 key」而静默退回兜底/裸 key 的情况；而**取词不在这里**：
// 控制器只交出事实，语言由渲染期（提示页的 tf / 模板的 tr）决定。
// 页面上的栏位标签与列值也不在这里：那些是 UI 文案，由模板取词（见 navigations.html）。
//
// 用别名而不是新造 struct：六个模块各造一份同形类型就是六份契约，字段名与参数编码
// 迟早分叉，而症状是「某个页面的提示偶尔少一个数字」，既不报错也不记日志
// （同 page 的 pageMsg）。
type navText = adminui.Msg
