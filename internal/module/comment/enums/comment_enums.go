package commentenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

const (
	StatusPending = "pending"

	StatusApproved = "approved"

	StatusRejected = "rejected"

	StatusSpam = "spam"
)

func Statuses() []string {
	return []string{StatusPending, StatusApproved, StatusRejected, StatusSpam}
}

func IsValidStatus(status string) bool {
	switch status {
	case StatusPending, StatusApproved, StatusRejected, StatusSpam:
		return true
	default:
		return false
	}
}

func ReviewableStatuses() []string {
	return []string{StatusApproved, StatusRejected}
}

func IsReviewableStatus(status string) bool {
	switch status {
	case StatusApproved, StatusRejected:
		return true
	default:
		return false
	}
}

const (
	MaxBodyLen = 2000

	MinBodyLen = 1

	MaxEntityTypeLen = 40

	MaxEntityIDLen = 64

	MaxKeywordLen = 64

	MaxAuthorIPHashLen = 64
)

const (
	DefaultPageSize = 20

	MaxPageSize = 50

	MaxReviewIDs = 200
)

const (
	ErrInvalidParam = "comment.err.invalidParam"

	ErrProjectRequired = "comment.err.projectRequired"

	ErrEntityTypeUnknown = "comment.err.entityTypeUnknown"

	ErrEntityIDInvalid = "comment.err.entityIDInvalid"

	ErrBodyRequired = "comment.err.bodyRequired"

	ErrBodyTooLong = "comment.err.bodyTooLong"

	ErrRateLimited = "comment.err.rateLimited"

	ErrNotAllowed = "comment.err.notAllowed"

	ErrLoginRequired = "comment.err.loginRequired"

	ErrCSRFRequired = "comment.err.csrfRequired"

	ErrUnavailable = "comment.err.unavailable"

	ErrNothingSelected = "comment.err.nothingSelected"

	ErrParentInvalid = "comment.err.parentInvalid"
)

const (
	MsgSubmitPending = "comment.msg.submitPending"

	MsgReviewApproved = "comment.msg.reviewApproved"

	MsgReviewRejected = "comment.msg.reviewRejected"
)

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

func Err(key string) error {
	return facing.New(key)
}

type LabelPair struct {
	Key      string
	Fallback string
}

const (
	LabelKeyStatusPending  = "admin.comment.status.pending"
	LabelKeyStatusApproved = "admin.comment.status.approved"
	LabelKeyStatusRejected = "admin.comment.status.rejected"
	LabelKeyStatusSpam     = "admin.comment.status.spam"

	LabelStatusPending  = "待审核"
	LabelStatusApproved = "已通过"
	LabelStatusRejected = "已驳回"
	LabelStatusSpam     = "垃圾"
)

func StatusLabel(status string) LabelPair {
	switch status {
	case StatusPending:
		return LabelPair{LabelKeyStatusPending, LabelStatusPending}
	case StatusApproved:
		return LabelPair{LabelKeyStatusApproved, LabelStatusApproved}
	case StatusRejected:
		return LabelPair{LabelKeyStatusRejected, LabelStatusRejected}
	case StatusSpam:
		return LabelPair{LabelKeyStatusSpam, LabelStatusSpam}
	default:
		return LabelPair{"", status}
	}
}

const (
	MsgPageTitle = "admin.comment.menu"

	LabelKeyColBody     = "admin.comment.col.body"
	LabelKeyColEntity   = "admin.comment.col.entity"
	LabelKeyColEntityID = "admin.comment.col.entity_id"
	LabelKeyColAuthor   = "admin.comment.col.author"
	LabelKeyColStatus   = "admin.comment.col.status"
	LabelKeyColTime     = "admin.comment.col.time"
	LabelKeyColActions  = "admin.comment.col.actions"

	LabelKeyFilterStatus     = "admin.comment.filter.status"
	LabelKeyFilterEntityType = "admin.comment.filter.entity_type"
	LabelKeyFilterKeyword    = "admin.comment.filter.keyword"
	LabelKeyFilterAll        = "admin.comment.filter.all"
	LabelKeyFilterSearch     = "admin.comment.filter.search"
	LabelKeyFilterReset      = "admin.comment.filter.reset"

	LabelKeyActionApprove = "admin.comment.action.approve"
	LabelKeyActionReject  = "admin.comment.action.reject"

	LabelKeyEmpty     = "admin.comment.empty"
	LabelKeyEmptyHint = "admin.comment.empty_hint"
	LabelKeyHint      = "admin.comment.hint"

	LabelKeyDoneApproved = "cmtDoneApprove"
	LabelKeyDoneRejected = "cmtDoneReject"

	LabelKeyErrNothingSelected = "admin.comment.err.nothing_selected"
	LabelKeyProjectRequired    = "admin.comment.project_required"
	LabelKeyNoProject          = "admin.comment.no_project"
	LabelKeyLoadFailed         = "admin.comment.load_failed"
)

var PageTitle = LabelPair{MsgPageTitle, "评论审核"}

const (
	LabelColBody     = "内容"
	LabelColEntity   = "评论对象"
	LabelColEntityID = "对象 id"
	LabelColAuthor   = "评论人"
	LabelColStatus   = "状态"
	LabelColTime     = "提交时间"
	LabelColActions  = "操作"

	LabelFilterStatus     = "状态"
	LabelFilterEntityType = "实体类型"
	LabelFilterKeyword    = "关键词"
	LabelFilterAll        = "全部"
	LabelFilterSearch     = "筛选"
	LabelFilterReset      = "重置"

	LabelActionApprove = "通过"
	LabelActionReject  = "驳回"

	LabelEmpty     = "没有符合条件的评论。"
	LabelEmptyHint = "换一个筛选条件，或等访客提交新的评论。"
	LabelHint      = "评论按「站点工程 + 实体」隔离；只有「已通过」的评论会出现在公开页面上。"

	LabelDoneApprovedTemplate = "已通过 {count} 条评论。"
	LabelDoneRejectedTemplate = "已驳回 {count} 条评论。"

	LabelErrNothingSelected = "没有选择任何评论。"
	LabelProjectRequired    = "请先选择站点工程：评论按工程隔离。"
	LabelNoProject          = "还没有站点工程：评论按工程隔离，先建一个工程再看这一页。"
	LabelLoadFailed         = "评论列表暂时读不出来 —— 稍后重试。"
)

func DoneTemplate(approved bool) LabelPair {
	if approved {
		return LabelPair{LabelKeyDoneApproved, LabelDoneApprovedTemplate}
	}
	return LabelPair{LabelKeyDoneRejected, LabelDoneRejectedTemplate}
}
