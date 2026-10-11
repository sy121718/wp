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
// 与 navigation_err.go 同形（同一套三种形态的判据），差别只在模块前缀：
//   - 整串相等   → 白名单命中，按当前语言取词；
//   - `key|param` → 带参协议，交给 response 的取词；
//   - `key：<定位>` → 本模块自己拼的形态（「这个工程没配默认等级：<project_id>」）。
//
// 两条出口，**取词责任不同**（这是本文件最容易混的一处）：
//   - JSON 出口 membershipErrText 只回**枚举 key**（带定位信息时归一成半角「: 」），
//     取词交给 pkg/response 的 translate，按**请求方**的语言解析 —— 前端要能按自己的
//     系统语言渲染，响应里就不能躺着一句服务端已经定好的中文；
//   - 页面出口 membershipErrPageText 直接回**当前语言下成品的一句**（页面是服务端渲染的，
//     语言由本次请求的 cookie 决定，模板只负责把它放进提示条）。
package membershiphttp

import (
	"go_wp/internal/middleware/builtin"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/internal/templates"
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
		return templates.TranslateFunc(response.RequestLanguage(c))(key, key) + facingSep(c) + detail
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
// 命中白名单 → 返回**枚举 key**（不是译文）；未命中 → 记结构化日志 + 归口 key。
//
// 为什么这里不取词：调用点紧接着就调 response.ErrorWithMessage(c, …)，它内部会把实参
// 交给 translate 按**本次请求**的语言解析。在这一层先翻一遍，等于把「已经定成服务端语言的
// 字符串」再交给 translate（那时它既不等于任何 key、也不含「: 」），一句话就此定型 ——
// 前端换语言、或运营用另一种语言打开，看到的还是服务端当时那句。
//
// 用带定位信息的形态时必须归一成半角「: 」（translate 只认这一种，见 pkg/response）：
// 本模块写侧固定拼全角「：」（enums.FacingDetailSep），不归一则整串都不命中词条，
// 响应里会原样出现 `membership.err.defaultTierMissing：42`。
func membershipErrText(c *gin.Context, err error) string {
	if msg, ok := membershipFacingText(err); ok {
		return facingKeyForResponse(msg)
	}
	logger.Scene(membershipErrScene).
		With("user_id", builtin.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "membership 接口内部错误")
	return membershipenums.ErrInternal
}

// facingKeyForResponse 把白名单命中的文案归一成 pkg/response 认得的形态：
// `key：<定位>`（全角，本模块写侧）→ `key: <定位>`（半角，translate 的形态 3）。
// 其余形态（纯 key / key|param）原样返回。
func facingKeyForResponse(msg string) string {
	if key, detail, ok := membershipenums.SplitFacingDetail(msg); ok {
		return key + membershipErrSepHalf + detail
	}
	return msg
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
		With("user_id", builtin.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "membership 后台页面内部错误")
	return templates.TranslateFunc(response.RequestLanguage(c))(membershipenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）")
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
