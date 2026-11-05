package pipeline

import (
	"go_wp/pkg/publish"
	"context"
	"fmt"
	"strings"

	"go_wp/pkg/builder"
	nav "go_wp/internal/builder/extensions/nav"
	"go_wp/pkg/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	json "go_wp/pkg/jsonx"
)

type FrozenBlock = publish.FrozenBlock


type ReferenceSnapshot struct {
	*publish.ReferenceSnapshot
}

// Unwrap 返回 pkg 的冻结数据，便于与本包的适配器互转。
func (s *ReferenceSnapshot) Unwrap() *publish.ReferenceSnapshot {
	if s == nil {
		return nil
	}
	return s.ReferenceSnapshot
}

// WrapReferenceSnapshot 给 pkg 冻结数据补上应用侧解析能力。
func WrapReferenceSnapshot(s *publish.ReferenceSnapshot) *ReferenceSnapshot {
	if s == nil {
		return nil
	}
	return &ReferenceSnapshot{ReferenceSnapshot: s}
}

// ResolveBlockRoot 用构建器解析冻结块文档，返回根节点。
func (s *ReferenceSnapshot) ResolveBlockRoot(id string) ([]*core.Node, error) {
	if s == nil || s.ReferenceSnapshot == nil {
		return nil, fmt.Errorf("全局块 %s 缺少冻结输入", id)
	}
	doc, err := s.ReferenceSnapshot.BlockDocument(id)
	if err != nil {
		return nil, err
	}
	page, err := builder.ParsePage(doc)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

type blockSnapshotReader interface {
	List(context.Context, *blockcontract.ListReq) ([]blockcontract.BlockResp, error)
}

type templateCapture struct {
	source   StructureTemplatePort
	snapshot *publish.ReferenceSnapshot
}

func (p *templateCapture) ResolveStructureDocument(ctx context.Context, projectID, id string) ([]byte, error) {
	if doc, ok := p.snapshot.Templates[id]; ok {
		return append([]byte(nil), doc...), nil
	}
	if p.source == nil {
		return nil, fmt.Errorf("结构模板 %s 未装配", id)
	}
	doc, err := p.source.ResolveStructureDocument(ctx, projectID, id)
	if err == nil {
		p.snapshot.Templates[id] = append(json.RawMessage(nil), doc...)
	}
	return doc, err
}

func FreezeReferences(ctx context.Context, projectID string, doc []byte, blocks blockSnapshotReader,
	templates StructureTemplatePort, navigation core.NavigationResolver) (*ReferenceSnapshot, error) {
	page, err := builder.ParsePage(doc)
	if err != nil {
		return nil, err
	}
	out := &publish.ReferenceSnapshot{ProjectID: projectID, Blocks: map[string]publish.FrozenBlock{}, Templates: map[string]json.RawMessage{}}
	var candidates map[string]blockcontract.BlockResp
	loaded := false
	var visit func([]*core.Node) error
	var useBlock func(string) error
	useBlock = func(id string) error {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil
		}
		if _, ok := out.Blocks[id]; ok {
			return nil
		}
		if !loaded {
			loaded = true
			candidates = map[string]blockcontract.BlockResp{}
			if blocks == nil {
				return fmt.Errorf("全局块 %s 未装配", id)
			}
			rows, e := blocks.List(ctx, &blockcontract.ListReq{ProjectID: projectID, ReuseMode: "global"})
			if e != nil {
				return e
			}
			for _, row := range rows {
				candidates[row.ID] = row
			}
		}
		b, ok := candidates[id]
		if !ok || (b.ProjectID != "" && b.ProjectID != projectID) || b.ReuseMode == "template" {
			return fmt.Errorf("全局块 %s 不可用", id)
		}
		parsed, e := builder.ParsePage(b.Document)
		if e != nil {
			return e
		}
		out.Blocks[id] = publish.FrozenBlock{Version: b.Version, Document: append(json.RawMessage(nil), b.Document...)}
		return visit(parsed.Root)
	}
	var menuBlocks func([]core.NavigationItem) error
	menuBlocks = func(items []core.NavigationItem) error {
		for _, item := range items {
			if e := useBlock(item.PanelBlockID); e != nil {
				return e
			}
			if e := menuBlocks(item.Children); e != nil {
				return e
			}
		}
		return nil
	}
	var inlineBlocks func([]nav.Item) error
	inlineBlocks = func(items []nav.Item) error {
		for _, item := range items {
			if e := useBlock(item.PanelBlockID); e != nil {
				return e
			}
			if e := inlineBlocks(item.Children); e != nil {
				return e
			}
		}
		return nil
	}
	visit = func(nodes []*core.Node) error {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			switch node.Type {
			case "core.globalref", "core.layoutSlot":
				var p struct {
					BlockID string `json:"blockId"`
				}
				if e := json.Unmarshal(node.Props, &p); e != nil {
					return e
				}
				if e := useBlock(p.BlockID); e != nil {
					return fmt.Errorf("节点 %s: %w", node.ID, e)
				}
			case "core.nav":
				var p nav.Props
				if e := json.Unmarshal(node.Props, &p); e != nil {
					return e
				}
				p.Navigation = strings.TrimSpace(p.Navigation)
				p.Menu = strings.TrimSpace(p.Menu)
				if navigation != nil && (p.Navigation != "" || p.Menu != "") {
					var items []core.NavigationItem
					var e error
					if p.Navigation != "" {
						items, e = navigation.ResolveNavigation(projectID, p.Navigation)
					} else {
						items, e = navigation.ResolveMenu(projectID, p.Menu)
					}
					if e != nil {
						return e
					}
					if e = menuBlocks(items); e != nil {
						return e
					}
				} else if e := inlineBlocks(p.Items); e != nil {
					return e
				}
			}
			if e := visit(node.Children); e != nil {
				return e
			}
		}
		return nil
	}
	if err = visit(page.Root); err != nil {
		return nil, err
	}
	plans, err := planStructureSlots(ctx, StructureSlotInput{Port: &templateCapture{source: templates, snapshot: out}, ProjectID: projectID, Structure: page.Settings.Structure, Mode: builder.CompileModePublish})
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if plan.TemplateID != "" {
			err = visit(plan.Nodes)
		} else {
			err = useBlock(plan.BlockID)
		}
		if err != nil {
			return nil, fmt.Errorf("结构槽位 %s（引用 %s）: %w", plan.Slot, plan.BlockID, err)
		}
	}
	return WrapReferenceSnapshot(out), nil
}

type referenceSnapshotKey struct{}

func WithReferenceSnapshot(ctx context.Context, snapshot *ReferenceSnapshot) context.Context {
	return context.WithValue(ctx, referenceSnapshotKey{}, snapshot.Clone().Unwrap())
}

// Clone 复制冻结数据（返回适配器形态）。
func (s *ReferenceSnapshot) Clone() *ReferenceSnapshot {
	if s == nil || s.ReferenceSnapshot == nil {
		return nil
	}
	return WrapReferenceSnapshot(s.ReferenceSnapshot.Clone())
}

func ReferenceSnapshotFromContext(ctx context.Context, projectID string) *ReferenceSnapshot {
	if s, _ := ctx.Value(referenceSnapshotKey{}).(*publish.ReferenceSnapshot); s != nil && s.ProjectID == projectID {
		return WrapReferenceSnapshot(s.Clone())
	}
	return nil
}
