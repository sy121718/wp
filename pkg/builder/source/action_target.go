package source

import "context"

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
