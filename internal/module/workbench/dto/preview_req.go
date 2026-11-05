package workbenchdto

import "go_wp/pkg/builder/source"

type PreviewStateReq struct {
	PreviewState source.PreviewState `form:"previewState" binding:"omitempty,oneof=default loading empty unauthenticated validation-error service-error submitting success out-of-stock variant-required forbidden not-found"`
}
