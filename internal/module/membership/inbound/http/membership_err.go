// membership_err.go — membership 模块的错误响应归口（错误文案三件套）。
//
// 三件套（AGENTS.md §响应与错误处理）：
//
//	① 白名单 —— enums.MembershipFacingMessages，与 enums 常量一一对应（AST 对账测试钉住）；
//	② 归口文案 —— 未命中时返回 enums.ErrInternal（可翻译 key）；
//	③ 结构化日志 —— 原文只进日志，带场景与 user_id。
//
// 为什么必须收口：service 一旦把 PostgreSQL 原文上抛（表名 / 索引名 uq_membership_tiers_name /
// SQLSTATE），它会随 500 原样直出给前端。响应不是可信边界。
// 门禁：scripts/check-no-internal-error-leak.sh。
//
// 页面写动作的结论由 membershipJump 渲染成**整页提示**（对应 ThinkPHP 的 success() / error()），
// 不再经 302 + `?err=` / `?done=` 回带 —— 那条通道要求读侧再判一次「这条提示是不是本仓给的」，
// 而查询参数不是可信边界；读侧判定（membershipNoticeTexts / membershipPageErr / membershipPageDone）
// 与回跳 URL 拼装（membershipErrURL / membershipOKURL）随之整批删除。
//
// 与 navigation_err.go 同形（同一套三种形态的判据），差别只在模块前缀：
//   - 整串相等   → 白名单命中，按当前语言取词；
//   - `key|param` → 带参协议，交给 response 的取词；
//   - `key：<定位>` → 本模块自己拼的形态（「这个工程没配默认等级：<project_id>」），
//     必须自己翻：response 的 translate 不认识全角「：」，不翻的话页面上会原样出现裸 key。
package membershiphttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	"go_wp/internal/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// membershipErrScene 日志场景名（与模块内其它 logger.Scene("membership") 一致）。
const membershipErrScene = "membership"

// membershipErrSepHalf 半角分隔符（非中文语言下的定位信息分隔符）。
const membershipErrSepHalf = ": "

// membershipFacingText 命中白名单 → 返回可对外文案（原文，可能是 key / key|param / key：定位）；
// 未命中 → ("", false)。**不记日志** —— 给页面路径用（那里已在调用点记了更具体的一条）。
func membershipFacingText(err error) (string, bool) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return membershipenums.HitFacingMessage(msg)
}

// localizeFacing 把白名单命中的文案转成当前语言下可直接渲染的一句。
func localizeFacing(c *gin.Context, msg string) string {
	if key, detail, ok := membershipenums.SplitFacingDetail(msg); ok {
		return shell.TranslateFor(c)(key, key) + facingSep(c) + detail
	}
	return response.TranslateMessage(c, msg)
}

// facingSep 定位信息分隔符：中文用全角冒号，其余语言用「: 」（与 service.FacingText 同口径）。
func facingSep(c *gin.Context) string {
	if strings.HasPrefix(strings.ToLower(response.RequestLanguage(c)), "zh") {
		return membershipenums.FacingDetailSep
	}
	return membershipErrSepHalf
}

// membershipErrText JSON 接口的错误文案出口。
//
// 命中白名单 → 业务文案；未命中 → 记结构化日志 + 归口文案。
func membershipErrText(c *gin.Context, err error) string {
	if msg, ok := membershipFacingText(err); ok {
		return localizeFacing(c, msg)
	}
	logger.Scene(membershipErrScene).
		With("user_id", shell.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "membership 接口内部错误")
	return membershipenums.ErrInternal
}

// membershipErrPageText 后台页面路径的归口文案（整页渲染 + 提示条，不走 pkg/response）。
//
// 与 JSON 出口分开的理由同 navigation：页面把文案拼进提示条，泄漏的是**页面正文**，
// 而 check-no-internal-error-leak.sh 的判据只看 response.ErrorWithMessage / c.String 的实参。
func membershipErrPageText(c *gin.Context, err error) string {
	if msg, ok := membershipFacingText(err); ok {
		return localizeFacing(c, msg)
	}
	logger.Scene(membershipErrScene).
		With("user_id", shell.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "membership 后台页面内部错误")
	return shell.TranslateFor(c)(membershipenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）")
}

// membershipErrStatus 把业务错误映射到 HTTP 状态码。
//
// 判据只看**业务 key**（不带定位信息的那一段），与白名单同一份读法：
// 404 类（不存在）→ 404；冲突类（撞名 / 撞门槛 / 已有默认）→ 409；
// 其余业务错误 → 400；未命中（内部错误）→ 500。
func membershipErrStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	key := err.Error()
	if k, _, ok := membershipenums.SplitFacingDetail(key); ok {
		key = k
	}
	switch key {
	case membershipenums.ErrNotFound:
		return http.StatusNotFound
	case membershipenums.ErrTierNameTaken, membershipenums.ErrThresholdTaken,
		membershipenums.ErrDefaultTierExists, membershipenums.ErrTierInUse,
		membershipenums.ErrDefaultTierRequired, membershipenums.ErrManualNotLocked:
		return http.StatusConflict
	case membershipenums.ErrRecalcUnavailable:
		return http.StatusServiceUnavailable
	case membershipenums.ErrInvalidParam, membershipenums.ErrEntitlementKind,
		membershipenums.ErrEntitlementValue, membershipenums.ErrThresholdInvalid,
		membershipenums.ErrUserRequired, membershipenums.ErrProjectRequired,
		membershipenums.ErrDefaultTierMissing:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// membershipTierBackKeys / membershipAssignBackKeys 写动作回跳要带回来的筛选上下文。
//
// 与模板里表单 action 的 query 逐键对应（页面渲染时拼进 action，POST 回来由
// shell.BackPath 从本次请求的 query 读回）—— 两处必须是同一份，否则会出现
// 「页面把某个筛选拼进去了、回跳时又丢掉」这种只在特定筛选下才暴露的差异。
//
// 层级页的上下文只有工程；归属页还有等级 / 来源 / 访客 / 页码四维筛选。
var (
	membershipTierBackKeys   = []string{"project"}
	membershipAssignBackKeys = []string{"project", "tierId", "source", "userId", "page"}
)

// membershipJump 页面写动作的统一出口：整页提示（对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302 + `?err=` / `?done=`：那条通道要求读侧再判一次「这条提示是不是本仓给的」
// （membershipNoticeTexts 的候选集合），而查询参数不是可信边界。现在文案走响应体，
// 读侧判定与回跳 URL 拼装随之整批删除。
//
// 提示文本必须**已过本模块白名单 / 已归口**（membershipErrPageText / membershipNotice 的产物），
// 原文只进日志 —— 换个页面呈现不等于可以把 err.Error() 铺在页面上。
//
// 失败不自动跳转（Seconds=0）：运营要看清楚原因。成功 1 秒后自动回列表页
// （与 sysconfig / order / plugin 同一取舍）。
func membershipJump(c *gin.Context, ok bool, text, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: text, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: text, Back: back, BackText: backText})
}

// membershipTierBackText / membershipAssignBackText 提示页链接的文字。
//
// 复用两个页面的标题词条而不是新造 `*.action.back`：新增词条要走 seed 迁移，
// 而这一句的语义就是「去这一页」（同 order 的 couponBackText）。
func membershipTierBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(membershipenums.PageTiersTitle, "会员等级与权益")
}

func membershipAssignBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(membershipenums.PageAssignmentsTitle, "会员归属")
}

// membershipSourceLabelKey 归属来源的展示用词条 key。
//
// 放在本文件（而非 enums）：它只在展示层用，且必须与 sourceValue 的两个取值一一对应。
func membershipSourceLabelKey(source string) (key, fallback string) {
	switch source {
	case membershipmodel.SourceManual:
		return membershipenums.SourceLabelManual, "手工指定"
	default:
		return membershipenums.SourceLabelAuto, "自动重算"
	}
}
