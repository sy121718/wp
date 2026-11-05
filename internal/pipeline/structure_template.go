package pipeline

import (
	"go_wp/pkg/publish"
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/logger"
)

type StructureTemplatePort interface {
	ResolveStructureDocument(ctx context.Context, projectID, templateID string) ([]byte, error)
}

type structureSlotResolver struct {
	inner core.BlockResolver
	refs  map[string][]*core.Node

	failed map[string]string
}

func (r *structureSlotResolver) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	if nodes, ok := r.refs[blockID]; ok {
		return nodes, nil
	}
	if reason, ok := r.failed[blockID]; ok {
		return nil, &core.RefDegradeError{Reason: reason, Err: fmt.Errorf("结构模板引用 %s 未展开", blockID)}
	}
	if r.inner == nil {
		return nil, &core.RefDegradeError{Reason: core.RefReasonResolverMissing, Err: errors.New("块解析器未注入")}
	}
	return r.inner.ResolveBlockRoot(blockID)
}

type structureSlotPlan struct {
	Slot string

	BlockID string

	TemplateID string

	Nodes []*core.Node

	FailedReason string
}

func (p structureSlotPlan) deps() []publish.Dependency {
	if p.FailedReason != "" {

		return nil
	}
	if p.TemplateID == "" {
		k := publish.BlockKey(p.BlockID)
		return []publish.Dependency{{Kind: k.Kind, Key: k.Key}}
	}
	k := publish.ContentTemplateKey(p.TemplateID)
	out := []publish.Dependency{{Kind: k.Kind, Key: k.Key}}

	for _, blockID := range builder.ReferencedBlockIDs(p.Nodes) {
		bk := publish.BlockKey(blockID)
		out = append(out, publish.Dependency{Kind: bk.Kind, Key: bk.Key})
	}
	return out
}

type StructureSlotInput struct {
	Port StructureTemplatePort

	ProjectID string

	Structure builder.StructureBindings

	Inner core.BlockResolver

	Mode builder.CompileMode

	Diagnostics *builder.DegradeCollector
}

type StructureSlotResult struct {
	Slots []builder.StructureSlot

	Resolver core.BlockResolver

	Deps []publish.Dependency
}

func planStructureSlots(ctx context.Context, in StructureSlotInput) ([]structureSlotPlan, error) {
	templateBindings := in.Structure.TemplateBindings()
	blockBindings := in.Structure.SlotBindings()
	if len(templateBindings) == 0 && len(blockBindings) == 0 {
		return nil, nil
	}

	slotSet := map[string]string{}
	for slot := range blockBindings {
		slotSet[slot] = ""
	}
	for slot := range templateBindings {
		slotSet[slot] = ""
	}
	var out []structureSlotPlan
	for _, slot := range builder.SortedSlots(slotSet) {

		if templateID := strings.TrimSpace(templateBindings[slot]); templateID != "" {
			nodes, terr := structureTemplateNodes(ctx, in.Port, in.ProjectID, templateID)
			switch {
			case terr != nil:

				if in.Mode == builder.CompileModePublish {
					return nil, fmt.Errorf("结构槽位 %s 绑定的结构模板 %s 不可用（%s）：发布被拒绝，避免上线缺结构的不完整页面",
						slot, templateID, structureTemplateErrText(terr))
				}
				in.Diagnostics.Record(builder.Degrade{
					Slot: slot, Kind: builder.DegradeKindTemplate, RefID: templateID,
					Reason: core.RefReasonTemplateUnavailable,
				})
				if blockID := strings.TrimSpace(blockBindings[slot]); blockID != "" {
					logger.Scene("build").With("slot", slot).With("template_id", templateID).
						With("err", structureTemplateErrText(terr)).
						Warn("结构模板不可用，预览回退到该槽位的块绑定")
					out = append(out, structureSlotPlan{Slot: slot, BlockID: blockID})
					continue
				}
				logger.Scene("build").With("slot", slot).With("template_id", templateID).
					With("err", structureTemplateErrText(terr)).
					Warn("结构模板不可用且无块可回退，预览输出带归因的槽位占位")
				out = append(out, structureSlotPlan{
					Slot: slot, TemplateID: templateID,
					BlockID:      builder.StructureTemplateRef(templateID),
					FailedReason: core.RefReasonTemplateUnavailable,
				})
				continue
			case len(nodes) == 0:

				in.Diagnostics.Record(builder.Degrade{
					Slot: slot, Kind: builder.DegradeKindTemplate, RefID: templateID,
					Reason: core.RefReasonTemplateEmpty,
				})
				logger.Scene("build").With("slot", slot).With("template_id", templateID).
					Warn("结构模板没有任何内容，回退到该槽位的块绑定")
			default:
				out = append(out, structureSlotPlan{
					Slot: slot, TemplateID: templateID,
					BlockID: builder.StructureTemplateRef(templateID), Nodes: nodes,
				})
				continue
			}
		}

		if blockID := strings.TrimSpace(blockBindings[slot]); blockID != "" {
			out = append(out, structureSlotPlan{Slot: slot, BlockID: blockID})
		}
	}
	return out, nil
}

func BuildStructureSlots(ctx context.Context, in StructureSlotInput) (StructureSlotResult, error) {
	plans, err := planStructureSlots(ctx, in)
	if err != nil {
		return StructureSlotResult{}, err
	}
	if len(plans) == 0 {
		return StructureSlotResult{Resolver: in.Inner}, nil
	}
	var out StructureSlotResult
	refs := map[string][]*core.Node{}
	failed := map[string]string{}
	for _, p := range plans {
		out.Slots = append(out.Slots, builder.StructureSlot{Slot: p.Slot, BlockID: p.BlockID})
		out.Deps = append(out.Deps, p.deps()...)
		if p.TemplateID == "" {
			continue
		}
		if p.FailedReason != "" {
			failed[p.BlockID] = p.FailedReason
			continue
		}
		refs[p.BlockID] = p.Nodes
	}
	if len(refs) == 0 && len(failed) == 0 {
		out.Resolver = in.Inner
		return out, nil
	}
	out.Resolver = &structureSlotResolver{inner: in.Inner, refs: refs, failed: failed}
	return out, nil
}

func StructureSlotDependencies(ctx context.Context, port StructureTemplatePort, projectID string,
	structure builder.StructureBindings) []publish.Dependency {
	plans, _ := planStructureSlots(ctx, StructureSlotInput{
		Port: port, ProjectID: projectID, Structure: structure,
	})
	var out []publish.Dependency
	for _, p := range plans {
		out = append(out, p.deps()...)
	}
	return out
}

func structureTemplateNodes(ctx context.Context, port StructureTemplatePort, projectID, templateID string) ([]*core.Node, error) {
	if port == nil {
		return nil, errors.New("结构模板端口未装配")
	}
	doc, err := port.ResolveStructureDocument(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	if len(doc) == 0 {
		return nil, errors.New("结构模板无可用文档")
	}
	parsed, err := builder.ParsePage(doc)
	if err != nil {
		return nil, err
	}
	return parsed.Root, nil
}

func structureTemplateErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
