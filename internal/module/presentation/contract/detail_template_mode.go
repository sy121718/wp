package presentationcontract

import (
	"context"

	presentationdto "go_wp/internal/module/presentation/dto"
)

type DetailTemplateModePort interface {
	SaveOverrideDocument(ctx context.Context, req *presentationdto.SaveOverrideReq) (res *presentationdto.InstanceResp, err error)

	ReapplyPreset(ctx context.Context, req *presentationdto.ReapplyPresetReq) (res *presentationdto.InstanceResp, err error)

	RollbackDocument(ctx context.Context, req *presentationdto.RollbackDocumentReq) (res *presentationdto.InstanceResp, err error)

	RollbackArtifact(ctx context.Context, req *presentationdto.RollbackArtifactReq) (res *presentationdto.InstanceResp, err error)

	ListSnapshots(ctx context.Context, req *presentationdto.ListSnapshotsReq) (list []*presentationdto.SnapshotSummary, err error)

	CountByTemplate(ctx context.Context, req *presentationdto.CountByTemplateReq) (res *presentationdto.CountByTemplateResp, err error)
}
