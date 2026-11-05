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

func Err(key string) error { return facing.New(key) }

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}
