// membership_err.go — membership 模块的错误响应归口（错误文案三件套的读侧）。
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
//   - `key：<定位>` → 本模块自己拼的形态（「这个工程没配默认等级：<project_id>」），
//     必须自己翻：response 的 translate 不认识全角「：」，不翻的话页面上会原样出现裸 key。
package membershiphttp

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	"go_wp/internal/web/shell"
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

// membershipNoticeTexts 本模块页面上可以原样展示的回执文案（当前语言）。
//
// 写侧（?err= / ?done= 回带）与读侧共用这一份：读侧按形状**整体**匹配，
// 未命中的 query 参数会被当伪造文案丢掉（查询参数不是可信边界）。
func membershipNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0, (len(membershipenums.MembershipFacingMessages)+3)*2)
	for _, key := range membershipenums.MembershipFacingMessages {
		out = append(out, key, tr(key, key))
	}
	out = append(out,
		tr(membershipenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）"),
		// 逐条成功回执的译文：写侧回带的是译文，读侧必须能认出来。
		tr(membershipenums.MsgTierCreated, membershipenums.MsgTierCreated),
		tr(membershipenums.MsgTierUpdated, membershipenums.MsgTierUpdated),
		tr(membershipenums.MsgTierDeleted, membershipenums.MsgTierDeleted),
		tr(membershipenums.MsgEntitlementSaved, membershipenums.MsgEntitlementSaved),
		tr(membershipenums.MsgAssignSet, membershipenums.MsgAssignSet),
		tr(membershipenums.MsgAssignUnlocked, membershipenums.MsgAssignUnlocked),
		shell.BulkIDsNoticeTemplate(c),
	)
	return out
}

// membershipPageErr 列表页 ?err= 的统一出口（未命中落归口文案）。
func membershipPageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, membershipNoticeTexts(c))
	})
}

// membershipPageDone 列表页 ?done= 的统一出口（成功提示：未命中落空串）。
func membershipPageDone(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("done"), "", func(raw string) string {
		return shell.FacingNotice(raw, membershipNoticeTexts(c))
	})
}

// membershipErrURL 页面写失败的回跳地址（带 ?err= 业务文案）。
//
// 回带的是**已过白名单的成品文案**（membershipErrPageText），不是 err.Error()：
// 内部错误因此只以归口文案出现在 URL 里，原文留在日志。
func membershipErrURL(c *gin.Context, base, projectID string, err error) string {
	target := base
	if projectID != "" {
		target += "?project=" + url.QueryEscape(projectID) + "&err=" + url.QueryEscape(membershipErrPageText(c, err))
		return target
	}
	return target + "?err=" + url.QueryEscape(membershipErrPageText(c, err))
}

// membershipOKURL 页面写成功的回跳地址（带 ?done= 成品回执）。
func membershipOKURL(base, projectID, notice string) string {
	target := base
	if projectID != "" {
		target += "?project=" + url.QueryEscape(projectID)
		if notice != "" {
			target += "&done=" + url.QueryEscape(notice)
		}
		return target
	}
	if notice != "" {
		target += "?done=" + url.QueryEscape(notice)
	}
	return target
}

// errMembershipParam 参数级校验失败（判定就在 handle 里，没有 error 对象）。
//
// 与 navigation 的同名助手同一理由：参数级失败也要落在白名单的候选里，
// 否则读侧会把回带的文案当伪造文案丢掉（页面上什么都不显示，也没有任何日志）。
func errMembershipParam() error { return errors.New(membershipenums.ErrInvalidParam) }

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
