package membershipcontract

import (
	"context"

	membershipdto "go_wp/internal/module/membership/dto"
)

type Reader interface {
	Resolve(ctx context.Context, req *membershipdto.ResolveReq) (res *membershipdto.MembershipResp, err error)
}

type Assigner interface {
	AssignManual(ctx context.Context, req *membershipdto.AssignManualReq) (res *membershipdto.AssignmentResp, err error)

	UnlockManual(ctx context.Context, req *membershipdto.UnlockManualReq) (err error)
}

type AssignmentAdminPort interface {
	ListAssignments(ctx context.Context, req *membershipdto.ListAssignmentsReq) (list []*membershipdto.AssignmentResp, err error)

	CountAssignments(ctx context.Context, req *membershipdto.CountAssignmentsReq) (total int64, err error)
}

type TierAdminPort interface {
	ListTiers(ctx context.Context, req *membershipdto.ListTiersReq) (list []*membershipdto.TierResp, err error)

	GetTier(ctx context.Context, req *membershipdto.GetTierReq) (res *membershipdto.TierResp, err error)

	CreateTier(ctx context.Context, req *membershipdto.CreateTierReq) (res *membershipdto.TierResp, err error)

	UpdateTier(ctx context.Context, req *membershipdto.UpdateTierReq) (res *membershipdto.TierResp, err error)

	DeleteTier(ctx context.Context, req *membershipdto.DeleteTierReq) (err error)

	SaveEntitlements(ctx context.Context, req *membershipdto.SaveEntitlementsReq) (res *membershipdto.SaveEntitlementsResp, err error)
}

type PurchaseSource interface {
	SpentTotalsByUser(ctx context.Context, projectID string) (totals map[uint64]int64, err error)
}

type RecalcPort interface {
	SetPurchaseSource(port PurchaseSource)

	// StartRecalcScheduler 返回的 stop 取消日结并等待在途一轮结束。
	StartRecalcScheduler(ctx context.Context) func()

	RecalcProject(ctx context.Context, req *membershipdto.RecalcProjectReq) (res *membershipdto.RecalcResult, err error)
}

type MembershipService interface {
	Reader
	Assigner
	AssignmentAdminPort
	TierAdminPort
	RecalcPort
}

type QueryReader interface {
	ListTiers(ctx context.Context, req *membershipdto.ListTiersReq) (list []*membershipdto.TierResp, err error)

	ListAssignments(ctx context.Context, req *membershipdto.ListAssignmentsReq) (list []*membershipdto.AssignmentResp, err error)
}

type Writer interface {
	Assigner
	TierAdminPort
}
