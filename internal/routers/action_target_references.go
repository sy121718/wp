package routers

import (
	"context"
	"go_wp/internal/builder/source"
	"sort"
)

type actionTargetReferences struct {
	readers []source.ActionTargetReferenceReader
}

func (r actionTargetReferences) ListActionTargetReferences(ctx context.Context, projectID, kind, id string) ([]source.ActionTargetReference, error) {
	seen := map[source.ActionTargetReference]bool{}
	for _, reader := range r.readers {
		refs, err := reader.ListActionTargetReferences(ctx, projectID, kind, id)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			seen[ref] = true
		}
	}
	out := make([]source.ActionTargetReference, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceKind != out[j].SourceKind {
			return out[i].SourceKind < out[j].SourceKind
		}
		return out[i].SourceID < out[j].SourceID
	})
	return out, nil
}

func wireActionTargetReferences(pages, blocks, templates, presentations any, invalidator source.ActionTargetInvalidator) {
	all := actionTargetReferences{}
	for _, candidate := range []any{pages, blocks, templates, presentations} {
		reader, ok := candidate.(source.ActionTargetReferenceReader)
		if !ok {
			panic("发布源未提供显式目标引用读取")
		}
		all.readers = append(all.readers, reader)
	}
	for _, candidate := range []any{pages, presentations} {
		setter, ok := candidate.(interface {
			SetActionTargetReferences(source.ActionTargetReferenceReader)
			SetActionTargetInvalidator(source.ActionTargetInvalidator)
		})
		if !ok {
			panic("发布源未提供显式目标保护与失效注入")
		}
		setter.SetActionTargetReferences(all)
		setter.SetActionTargetInvalidator(invalidator)
	}
}
