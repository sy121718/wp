package membershipenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

const (
	MsgTierCreated      = "membership.msg.tierCreated"
	MsgTierUpdated      = "membership.msg.tierUpdated"
	MsgTierDeleted      = "membership.msg.tierDeleted"
	MsgEntitlementSaved = "membership.msg.entitlementSaved"
	MsgAssignSet        = "membership.msg.assignSet"
	MsgAssignUnlocked   = "membership.msg.assignUnlocked"
)

const (
	ErrInvalidParam = "membership.err.invalidParam"

	ErrNotFound = "membership.err.notFound"

	ErrTierNameTaken = "membership.err.tierNameTaken"

	ErrThresholdTaken = "membership.err.thresholdTaken"

	ErrDefaultTierExists = "membership.err.defaultTierExists"

	ErrDefaultTierRequired = "membership.err.defaultTierRequired"

	ErrDefaultTierMissing = "membership.err.defaultTierMissing"

	ErrTierInUse = "membership.err.tierInUse"

	ErrEntitlementKind = "membership.err.entitlementKind"

	ErrEntitlementValue = "membership.err.entitlementValue"

	ErrThresholdInvalid = "membership.err.thresholdInvalid"

	ErrUserRequired = "membership.err.userRequired"

	ErrProjectRequired = "membership.err.projectRequired"

	ErrManualNotLocked = "membership.err.manualNotLocked"

	ErrRecalcUnavailable = "membership.err.recalcUnavailable"
)

const (
	KindFreeShipping = "free_shipping"

	KindDiscount = "discount"
)

const (
	SourceAuto = "auto"

	SourceManual = "manual"
)

const (
	PageTiersTitle       = "admin.membership.title"
	PageAssignmentsTitle = "admin.membership.assign.title"
)

var EntitlementKinds = []string{KindFreeShipping, KindDiscount}

var errFallbacks = map[string]string{
	ErrInvalidParam:        "参数不合法：请检查后重试。",
	ErrNotFound:            "会员记录不存在：可能已被删除。",
	ErrTierNameTaken:       "等级名已被占用：同工程内不能重名。",
	ErrThresholdTaken:      "门槛值已被占用：同一工程内两个等级不能同门槛。",
	ErrDefaultTierExists:   "该工程已有默认等级：请先取消原有的默认等级。",
	ErrDefaultTierRequired: "该工程必须保留一个默认等级：请先把另一档设为默认。",
	ErrDefaultTierMissing:  "该工程还没有默认等级：请先在等级设置里指定一个。",
	ErrTierInUse:           "等级上还挂着会员归属：请先转移或解除这些归属再删除。",
	ErrEntitlementKind:     "权益类型不在支持范围内：只支持免运费与折扣。",
	ErrEntitlementValue:    "权益取值越界：免运费只接受 0/1，折扣只接受 1..100。",
	ErrThresholdInvalid:    "门槛值不合法：不能是负数。",
	ErrUserRequired:        "缺少会员账号：请重新选择后再操作。",
	ErrProjectRequired:     "缺少工程：等级与归属都按工程划分，请先选定工程。",
	ErrManualNotLocked:     "该归属不是手工指定的：没有可解除的锁定。",
	ErrRecalcUnavailable:   "归属重算暂不可用：订单侧消费额聚合尚未就绪，请稍后重试。",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

func FallbackFor(key string) string { return errFallbacks[key] }
