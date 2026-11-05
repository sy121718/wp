package publish

import (
	"context"
	"fmt"

	json "go_wp/pkg/jsonx"
)

// FrozenBlock 是本次发布冻结的全局块输入。
type FrozenBlock struct {
	Version  int64           `json:"version"`
	Document json.RawMessage `json:"document"`
}

// ReferenceSnapshot 是发布时冻结的引用快照（块与结构模板）。
//
// 它只承载冻结输入事实；解析文档需要编译器的能力，由应用侧负责。
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

// BlockDocument 返回冻结块的文档字节，供应用侧用编译器的解析能力还原根节点。
func (s *ReferenceSnapshot) BlockDocument(id string) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("全局块 %s 缺少冻结输入", id)
	}
	block, ok := s.Blocks[id]
	if !ok {
		return nil, fmt.Errorf("全局块 %s 不在本次冻结输入中", id)
	}
	return append([]byte(nil), block.Document...), nil
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
