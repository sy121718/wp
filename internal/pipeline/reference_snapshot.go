package pipeline

import (
	"context"
	"fmt"
	"strings"

	"go_wp/internal/builder"
	nav "go_wp/internal/builder/components/nav"
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	json "go_wp/pkg/jsonx"
)

// FrozenBlock 是本次编译实际引用的公开配置，版本与文档同时固定。
type FrozenBlock struct {
	Version  int64           `json:"version"`
	Document json.RawMessage `json:"document"`
}

// ReferenceSnapshot 只含公开构建配置；nil 表示尚未冻结，空映射表示已冻结且没有引用。
// 缺失项必须失败，不允许在编译期间回读最新值。
type ReferenceSnapshot struct {
	ProjectID string                     `json:"projectId"`
	Blocks    map[string]FrozenBlock     `json:"blocks"`
	Templates map[string]json.RawMessage `json:"templates,omitempty"`
}

func (s *ReferenceSnapshot) Clone() *ReferenceSnapshot {
	if s == nil {
		return nil
	}
	out := &ReferenceSnapshot{ProjectID: s.ProjectID, Blocks: map[string]FrozenBlock{}}
	for id, b := range s.Blocks {
		b.Document = append(json.RawMessage(nil), b.Document...)
		out.Blocks[id] = b
	}
	if s.Templates != nil {
		out.Templates = map[string]json.RawMessage{}
		for id, doc := range s.Templates {
			out.Templates[id] = append(json.RawMessage(nil), doc...)
		}
	}
	return out
}

func (s *ReferenceSnapshot) ResolveBlockRoot(id string) ([]*core.Node, error) {
	if s == nil {
		return nil, fmt.Errorf("全局块 %s 缺少冻结输入", id)
	}
	block, ok := s.Blocks[id]
	if !ok {
		return nil, fmt.Errorf("全局块 %s 不在本次冻结输入中", id)
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

func (s *ReferenceSnapshot) ResolveStructureDocument(_ context.Context, projectID, id string) ([]byte, error) {
	if s.ProjectID != projectID {
		return nil, fmt.Errorf("结构模板 %s 不属于本次冻结工程", id)
	}
	doc, ok := s.Templates[id]
	if !ok {
		return nil, fmt.Errorf("结构模板 %s 不在本次冻结输入中", id)
	}
	return append([]byte(nil), doc...), nil
}

type blockSnapshotReader interface {
	List(context.Context, *blockcontract.ListReq) ([]blockcontract.BlockResp, error)
}

type templateCapture struct {
	source   StructureTemplatePort
	snapshot *ReferenceSnapshot
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

// FreezeReferences 在编译前固定递归引用闭包。候选块由现有 List 单条 SELECT 读取，
// 所以嵌套块不可能分别读到保存事务前后两个状态；无关块不会写入发布计划。
func FreezeReferences(ctx context.Context, projectID string, doc []byte, blocks blockSnapshotReader,
	templates StructureTemplatePort, navigation core.NavigationResolver) (*ReferenceSnapshot, error) {
	page, err := builder.ParsePage(doc)
	if err != nil {
		return nil, err
	}
	out := &ReferenceSnapshot{ProjectID: projectID, Blocks: map[string]FrozenBlock{}, Templates: map[string]json.RawMessage{}}
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
		} // 环与最大深度仍由既有编译器判定。
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
		out.Blocks[id] = FrozenBlock{Version: b.Version, Document: append(json.RawMessage(nil), b.Document...)}
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
	return out, nil
}

type referenceSnapshotKey struct{}

func WithReferenceSnapshot(ctx context.Context, snapshot *ReferenceSnapshot) context.Context {
	return context.WithValue(ctx, referenceSnapshotKey{}, snapshot.Clone())
}
func ReferenceSnapshotFromContext(ctx context.Context, projectID string) *ReferenceSnapshot {
	if s, _ := ctx.Value(referenceSnapshotKey{}).(*ReferenceSnapshot); s != nil && s.ProjectID == projectID {
		return s.Clone()
	}
	return nil
}
