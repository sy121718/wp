package workbenchdto

import "go_wp/internal/builder/source"

// PreviewStateReq 仅供临时画布请求；正式保存与发布不接受模拟状态。
type PreviewStateReq struct {
	PreviewState source.PreviewState `form:"previewState" binding:"omitempty,oneof=default loading empty unauthenticated validation-error service-error submitting success out-of-stock variant-required forbidden not-found"`
}
