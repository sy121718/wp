package pubcontract

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
	pubmodel "go_wp/internal/module/publication/model"

	"gorm.io/gorm"
)

const (
	ReceiptActionSwitchActive = pubdto.ReceiptActionSwitchActive
	ReceiptActionUpdateURL    = pubdto.ReceiptActionUpdateURL
	ReceiptActionRollback     = pubdto.ReceiptActionRollback

	ReceiptActionDeactivate = pubdto.ReceiptActionDeactivate

	ReceiptSourcePage         = pubdto.ReceiptSourcePage
	ReceiptSourcePresentation = pubdto.ReceiptSourcePresentation

	ReceiptSourceProject = pubdto.ReceiptSourceProject
)

type (
	ActivateReq                      = pubdto.ActivateReq
	DeactivateReq                    = pubdto.DeactivateReq
	RedirectReq                      = pubdto.RedirectReq
	RenameReservedReq                = pubdto.RenameReservedReq
	ReserveReq                       = pubdto.ReserveReq
	DeleteRoutesReq                  = pubdto.DeleteRoutesReq
	DeleteRoutesByPresentationReq    = pubdto.DeleteRoutesByPresentationReq
	ListActivePathsByPresentationReq = pubdto.ListActivePathsByPresentationReq
	ListActivePathsReq               = pubdto.ListActivePathsReq
	IsOccupiedReq                    = pubdto.IsOccupiedReq
	RouteResp                        = pubdto.RouteResp

	BeginPublishReceiptReq = pubdto.BeginPublishReceiptReq
	ReceiptPathIdentity    = pubdto.ReceiptPathIdentity

	ReceiptOwner       = pubdto.ReceiptOwner
	PendingReceiptResp = pubdto.PendingReceiptResp
	ReceiptsQueryReq   = pubdto.ReceiptsQueryReq

	RouteOwnerResp = pubdto.RouteOwnerResp
)

type ReceiptAttemptVerdict = pubdto.ReceiptAttemptVerdict

const (
	ReceiptAttemptNoEvidence ReceiptAttemptVerdict = pubdto.ReceiptAttemptNoEvidence
	ReceiptAttemptInProgress ReceiptAttemptVerdict = pubdto.ReceiptAttemptInProgress
	ReceiptAttemptSucceeded  ReceiptAttemptVerdict = pubdto.ReceiptAttemptSucceeded
	ReceiptAttemptFailed     ReceiptAttemptVerdict = pubdto.ReceiptAttemptFailed
)

type PublicationService interface {
	Activate(ctx context.Context, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error)

	Deactivate(ctx context.Context, req *pubdto.DeactivateReq) (err error)

	Redirect(ctx context.Context, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error)

	RollbackReceipts(ctx context.Context) (count int64, err error)

	BeginPublishReceipt(ctx context.Context, req *pubdto.BeginPublishReceiptReq) (receiptID string, err error)

	CompletePublishReceipt(ctx context.Context, receiptID string) (err error)

	AbortPublishReceipt(ctx context.Context, receiptID string) (err error)

	ListPendingReceipts(ctx context.Context) (list []pubdto.PendingReceiptResp, err error)

	ClaimPendingReceipts(ctx context.Context, req *pubdto.ReceiptsQueryReq) (list []pubdto.PendingReceiptResp, err error)

	CountPendingReceipts(ctx context.Context, req *pubdto.ReceiptsQueryReq) (n int64, err error)

	RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error)

	ReservePath(ctx context.Context, req *pubdto.ReserveReq) (err error)

	RefreshSiteFiles(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang, siteLangMode, notFoundHTML string) (err error)

	DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error)

	ListActivePathsByPresentation(ctx context.Context, req *pubdto.ListActivePathsByPresentationReq) (paths []string, err error)

	DeleteRoutesByPresentation(ctx context.Context, req *pubdto.DeleteRoutesByPresentationReq) (err error)

	ListActivePaths(ctx context.Context, req *pubdto.ListActivePathsReq) (paths []string, err error)

	IsPathOccupied(ctx context.Context, req *pubdto.IsOccupiedReq) (occupied bool, err error)

	ListReferencedArtifactIDs(ctx context.Context) (ids []string, err error)

	ActivateTx(ctx context.Context, tx *gorm.DB, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error)
	DeactivateTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeactivateReq) (err error)
	RedirectTx(ctx context.Context, tx *gorm.DB, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error)
	RenameReservedTx(ctx context.Context, tx *gorm.DB, req *pubdto.RenameReservedReq) (err error)
	ReservePathTx(ctx context.Context, tx *gorm.DB, req *pubdto.ReserveReq) (err error)
	DeleteRoutesByPageTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeleteRoutesReq) (err error)

	DeleteRoutesByPresentationTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeleteRoutesByPresentationReq) (err error)

	ReceiptAttemptVerdictTx(ctx context.Context, tx *gorm.DB, links map[string]string,
		ownedLangs map[string]string) (pubdto.ReceiptAttemptVerdict, error)

	RouteOwnerTx(ctx context.Context, tx *gorm.DB, projectID, path string) (*pubdto.RouteOwnerResp, error)

	MarkDeactivationCommittedTx(ctx context.Context, tx *gorm.DB, owner pubdto.ReceiptOwner,
		projectID, receiptID string, pathArtifacts map[string]pubdto.ReceiptPathIdentity) error

	LoadDeactivationReceiptTx(ctx context.Context, tx *gorm.DB, owner pubdto.ReceiptOwner,
		projectID, receiptID string) (*pubmodel.ReceiptEntity, error)
}
