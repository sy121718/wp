package navigationhttp

// navigation_err.go — 导航错误的对外口径（JSON 出口 + 页面出口 + 逐条原因），判据只有一份。
//
// 「这条错误能不能对外说、说什么」由 pkg/facing.FactOf 给出（key / Fallback / Tail /
// Business）：模块不再自带白名单 + 拆分函数 + 逐条 switch —— 那是第二份真相，service
// 换措辞、或某条错误改由别的模块提供时就静默失配，症状是把业务文案吞成归口、或把内部
// 错误当文案透出，两边都不报错。门禁：scripts/check-no-internal-error-leak.sh。
//
// 两个出口的产物形态**不同，这是有意的**：
//   - JSON 出口（navigationErrText）只给**未翻译的形态**（key / key|param / key: detail），
//     取词交给 pkg/response 按请求语言做 —— 单一取词入口；
//   - 页面出口（navigationErrMsg / navInvalidParamMsg）产出**事实**（adminui.Msg：
//     key + 中文兜底 + %s 参数），取词发生在渲染期（提示页注入的 tf）。
//     控制器提前翻成整句意味着「中文环境全对、英文界面整句中文」。
//
// 本文件同时承载参数级提示（navInvalidParamMsg）、逐条原因（navigationReasonText）与
// Go 侧拼标签用的取词函数（navTr）：页面写路径与接口出口读的是同一份事实，
// 「同一个错误在接口与页面上说法一致」因此不是靠两边各写一遍。

import (
	"go_wp/internal/middleware/builtin"
	"strings"

	"go_wp/internal/adminui"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// navigationErrScene 日志场景名（与 navigation 模块其它 logger.Scene("navigation") 一致）。
const navigationErrScene = "navigation"

// navigationInternalKey 归口 i18n key：未归类失败（SQL / 表名 / 约束名 / 文件路径）的对外文案。
//
// 它**不是**业务错误 —— 不构成一条 error，只在出口处作为「取哪句话」的键使用；
// 归口错误由出口自己写 500 + 这句话，模块不会再把它当成业务错误返回。
// key 本身登记在 enums（控制器不写词条字面量，门禁 check-controller-i18n-literals.sh）。
const navigationInternalKey = navigationenums.InternalKey

// navigationInternalFallback 归口文案的中文兜底（词条缺席时用，页面不能显示裸 key）。
const navigationInternalFallback = "操作失败，请稍后重试（细节只进日志）"

// navigationDetailSep 定位信息的分隔符：中文用全角冒号，其余语言用「: 」。
//
// 读侧 shell.FacingNotice 的形态 3 两种都认，所以写读两侧不会因语言切换而失配。
func navigationDetailSep(c *gin.Context) string {
	if strings.HasPrefix(response.RequestLanguage(c), "zh") {
		return "："
	}
	return ": "
}

// navigationFact 取错误事实；业务错误才返回 true。
//
// 只认 pkg/facing 的形状判据：自带文案的哨兵（enums.Err / ErrDetail）⇒ 业务事实；
// 裸业务 key ⇒ 业务事实；key + ASCII 冒号 + 明细 ⇒ 业务事实 + Tail；其余（SQL 原文、
// 文件路径）⇒ 内部错误。
func navigationFact(err error) (facing.Fact, bool) {
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		return facing.Fact{}, false
	}
	// 裸 key（errors.New(enums.ErrXxx)）的事实里兜底就是 key 本身 —— 那是「词条缺席时
	// 页面上显示裸 key」的老症状。本模块的兜底表与常量同处声明，在这里补上。
	if fact.Fallback == "" || fact.Fallback == fact.Key {
		key, _, _ := strings.Cut(fact.Key, "|")
		if text := navigationReasonFallback(key); text != "" {
			fact.Fallback = text
		}
	}
	return fact, true
}

// navigationErrText 把错误收敛成 JSON 接口要发的文案（**未翻译**）。
//
// 命中判据 → 原串（key / key|param / key: detail），由 response.ErrorWithMessage →
// pkg/response 按请求语言取词；未命中 → 记一条结构化日志并返回归口 key（同样由它翻）。
//
// 为什么这里不取词：翻完再交给 response 等于「翻两遍 + 自带第二套读法」（全角「：」与
// translate 认的半角「: 」就是现成的一例），而且「响应语言由谁决定」会有两个答案。
func navigationErrText(c *gin.Context, err error) string {
	fact, business := navigationFact(err)
	if !business {
		logger.Scene(navigationErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "navigation 接口内部错误")
		return navigationInternalKey
	}
	// 带定位信息时拼成 key + 半角分隔符 + 定位（translate 的形态 3）；其余形态原样。
	if fact.Tail != "" {
		return fact.Key + ": " + fact.Tail
	}
	return fact.Key
}

// navigationErrMsg HTML 页面路径的错误**事实**（整页渲染 + 提示条，不走 pkg/response）。
//
// 为什么单独收口：页面把 err.Error() 拼进提示条的形态（保存失败：+ 原文）泄漏的是
// **页面正文**，而 check-no-internal-error-leak.sh 的判据只看
// response.ErrorWithMessage / c.String 的实参 —— 这种形态它覆盖不到。
func navigationErrMsg(c *gin.Context, err error) adminui.Msg {
	fact, business := navigationFact(err)
	if !business {
		return adminui.Msg{Key: navigationInternalKey, Fallback: navigationInternalFallback}
	}
	return navigationFactMsg(c, fact)
}

// navigationFactMsg 业务事实 → 渲染期可消费的 adminui.Msg。
//
// 三档：key|param 保留参数形态（模板的 tf 与 adminui.TranslateArgs 同一实现）；
// 带定位信息的一段 key 表达不了「译文 + 分隔符 + 定位」，给成品串（Key 留空）；
// 其余给 key + 兜底，取词留在渲染期。
func navigationFactMsg(c *gin.Context, fact facing.Fact) adminui.Msg {
	if key, param, hasParam := strings.Cut(fact.Key, "|"); hasParam {
		return adminui.Msg{Key: key, Fallback: fact.Fallback}.WithArgs(param)
	}
	if fact.Tail != "" {
		return adminui.Msg{
			Fallback: adminui.TranslateArgs(c, fact.Key, fact.Fallback, nil) +
				navigationDetailSep(c) + fact.Tail,
		}
	}
	return adminui.Msg{Key: fact.Key, Fallback: fact.Fallback}
}

// navigationReasonFallbacks 不在业务错误兜底表里、但会出现在**逐条原因**里的界面文案兜底。
//
// 为什么另起一张表：这类 key 语义上不是「失败」（例：admin.navigations.no_public_path
// 「（暂无公开路径，先发布后再添加）」是一条**跳过原因**），塞进 enums 的 errFallbacks
// 会让那张表同时承担两种语义。取值与 sys_i18n 的 zh-CN 词条逐字一致（迁移 192）。
var navigationReasonFallbacks = map[string]string{
	navigationAddSourceNoPathKey: "（暂无公开路径，先发布后再添加）",
}

// navigationReasonFallback 逐条原因的兜底取值顺序：业务错误兜底表 → 界面文案兜底 → 空串
// （空串表示两张表都没有这个 key，交给 navigationInternalFallback 的通用最后一句）。
func navigationReasonFallback(key string) string {
	if text := navigationenums.FallbackFor(key); text != "" {
		return text
	}
	return navigationReasonFallbacks[key]
}

// navigationReasonText 把一条错误转成**逐条原因**（JSON 回执的 items[].message）要显示的成品句。
//
// 为什么归口到一处：删除端点（navigation_delete_write.go）与来源批量加入
// （navigation_add_source_write.go）的逐条原因都要「按请求语言出成品句 + 缺词不裸 key」。
func navigationReasonText(c *gin.Context, err error) string {
	fact, business := navigationFact(err)
	if !business {
		logger.Scene(navigationErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "navigation 接口内部错误")
		// 逐条原因本身是**成品句**（直接进 items[].message），所以这里就地取词：
		// 词条在场按请求语言出，缺席回落页面兜底句 —— 绝不能把裸 key 交给前端。
		if text := response.TranslateMessage(c, navigationInternalKey); text != navigationInternalKey {
			return text
		}
		return navigationInternalFallback
	}
	return navigationFactReasonText(c, fact)
}

// navigationFactReasonText 事实 → 逐条原因成品句（取词走 pkg/response，缺词走兜底表）。
func navigationFactReasonText(c *gin.Context, fact facing.Fact) string {
	if fact.Tail == "" {
		return navigationReasonKeyText(c, fact.Key)
	}
	text := response.TranslateMessage(c, fact.Key)
	if text == fact.Key {
		if fallback := navigationReasonFallback(fact.Key); fallback != "" {
			text = fallback
		}
	}
	return text + ": " + fact.Tail
}

// navigationReasonKeyText 同上，但入口是**已经归口好的文案**（key / key|param；没有 error 对象）。
//
// 为什么需要这一档：有些逐条原因压根没有 error 对象 —— 判定就发生在 handle 里
// （例：候选不属于本工程的该来源类型 → ErrNotFound；候选暂无公开路径 → 界面文案 key）。
func navigationReasonKeyText(c *gin.Context, msg string) string {
	if text := response.TranslateMessage(c, msg); text != msg {
		return text
	}
	return navigationReasonFallbackText(msg)
}

// navigationReasonFallbackText 词条缺席时的中文兜底（定位信息用半角分隔符拼回，
// 与 JSON 出口其它消息同一形态）。
func navigationReasonFallbackText(msg string) string {
	key := msg
	if k, _, ok := strings.Cut(msg, "|"); ok {
		// 带参形态：兜底句里没有占位符，参数没有位置可放，只保留 key 的兜底句。
		key = k
	}
	if text := navigationReasonFallback(key); text != "" {
		return text
	}
	return navigationInternalFallback
}

// navTr Go 侧取词函数（本模块**拼装标签**用的那一档；页面出口的取词一律发生在渲染期）。
//
// 为什么还需要它：有两处文案在渲染期之外就要成品串 ——
//
//	· panelBlockName：新建面板块的默认名**会落库**，必须按当前请求语言拼好；
//	· build：译文工作台的分组标题与错误槽由 Go 侧组装。
//
// 取词入口是共享设施（adminui.TranslateArgs），不是 shell.TranslateFor（正在退役的旧入口）。
func navTr(c *gin.Context) func(key, fallback string) string {
	return func(key, fallback string) string { return adminui.TranslateArgs(c, key, fallback, nil) }
}

// navNoticeItemMissing 编辑抽屉 / 归属核对查不到这一项时的结论（key + 中文兜底）。
//
// 与 service 的 ErrNotFound 是同一个词条，但**兜底不同**：service 那条说的是「请求资源不存在」，
// 这里要说清是**菜单项**不见了 —— 抽屉里冒出「请求资源不存在」时用户不知道说的是哪一层。
var navNoticeItemMissing = navText{Key: navigationenums.ErrNotFound, Fallback: "菜单项不存在"}

// navInvalidParamMsg 参数级校验失败的**事实**（key + 中文兜底，静态、不需要 gin.Context）。
//
// 参数级失败没有 error 对象（判定就在 handle 里），因此走不了 navigationErrMsg；
// 事实直接进提示页响应体，取词在渲染期发生。
var navInvalidParamMsg = navText{Key: navigationenums.ErrInvalidParam, Fallback: "参数错误"}

// navNoticeNoSourcePicked 「从已有内容添加」一项都没勾时的提示。
//
// 这是该抽屉最常见的一条路径（候选复选框默认全不勾，用户直接点「加入菜单」）。
// 模板侧另有「本组没有可加入项时按钮置灰」，这里是服务端兜底。
var navNoticeNoSourcePicked = navText{
	Key: navigationenums.NoticeNoSourcePicked, Fallback: "请至少勾选一项要加入菜单的内容。",
}

// navText 一条待取词文案的事实 —— 就是共享形状 adminui.Msg（key + 中文兜底 + %s 参数）。
//
// 本包所有由 Go 侧产出、会显示给用户的**结论/提示**文案都走它 —— 文案与 key 形影不离；
// 取词不在这里：控制器只交出事实，语言由渲染期（提示页的 tf / 模板的 tr）决定。
// 用别名而不是新造 struct：六个模块各造一份同形类型就是六份契约，字段名与参数编码迟早分叉。
type navText = adminui.Msg
