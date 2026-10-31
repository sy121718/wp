package globalref

import (
	"html"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, b *core.CSSBuckets) {}

type View struct {
	IsPlaceholder bool

	NodeID string

	Reason string

	RefID string

	Slot string
}

func BuildView(node *core.Node, block core.BlockResolver) (View, []*core.Node, error) {
	props, err := decode(node)
	if err != nil {
		return View{}, nil, err
	}

	placeholder := View{IsPlaceholder: true, NodeID: html.EscapeString(node.ID), RefID: props.BlockID}

	if block == nil {
		placeholder.Reason = core.RefReasonResolverMissing
		return placeholder, nil, nil
	}
	roots, err := block.ResolveBlockRoot(props.BlockID)
	if err != nil {

		placeholder.Reason = core.RefDegradeReason(err, core.RefReasonUnavailable)
		return placeholder, nil, nil
	}
	if len(roots) == 0 {

		placeholder.Reason = core.RefReasonEmpty
		return placeholder, nil, nil
	}

	prefix := node.ID + "-b-"
	out := core.CloneNodes(roots, func(id string) string { return prefix + id })
	return View{}, out, nil
}
