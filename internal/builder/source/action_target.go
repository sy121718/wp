package source

import "context"

// ActionTargetReference 是当前源码或活跃/暂存产物的目标引用事实。
type ActionTargetReference struct {
	SourceKind string `json:"sourceKind"`
	SourceID   string `json:"sourceId"`
}

type ActionTargetReferenceReader interface {
	ListActionTargetReferences(context.Context, string, string, string) ([]ActionTargetReference, error)
}

type ActionTargetInvalidator interface {
	Invalidate(context.Context, string, string)
}
