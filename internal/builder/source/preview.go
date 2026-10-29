package source

import "context"

// PreviewState 是临时预览请求的封闭展示状态，不进入Document或发布参数。
type PreviewState string

const (
	PreviewDefault         PreviewState = "default"
	PreviewLoading         PreviewState = "loading"
	PreviewEmpty           PreviewState = "empty"
	PreviewUnauthenticated PreviewState = "unauthenticated"
	PreviewValidationError PreviewState = "validation-error"
	PreviewServiceError    PreviewState = "service-error"
	PreviewSubmitting      PreviewState = "submitting"
	PreviewSuccess         PreviewState = "success"
	PreviewOutOfStock      PreviewState = "out-of-stock"
	PreviewVariantRequired PreviewState = "variant-required"
	PreviewForbidden       PreviewState = "forbidden"
	PreviewNotFound        PreviewState = "not-found"
)

func (state PreviewState) Valid() bool {
	switch state {
	case PreviewDefault, PreviewLoading, PreviewEmpty, PreviewUnauthenticated, PreviewValidationError, PreviewServiceError,
		PreviewSubmitting, PreviewSuccess, PreviewOutOfStock, PreviewVariantRequired, PreviewForbidden, PreviewNotFound:
		return true
	default:
		return false
	}
}

type previewStateContextKey struct{}

// WithPreviewState 只携带展示事实，不能授权访问真实私人数据或写业务。
func WithPreviewState(ctx context.Context, state PreviewState) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !state.Valid() {
		state = PreviewDefault
	}
	return context.WithValue(ctx, previewStateContextKey{}, state)
}

func PreviewStateFrom(ctx context.Context) PreviewState {
	if ctx != nil {
		if state, ok := ctx.Value(previewStateContextKey{}).(PreviewState); ok && state.Valid() {
			return state
		}
	}
	return PreviewDefault
}
