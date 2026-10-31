package builder

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"go_wp/internal/builder/core"
)

type CompileMode string

const (
	CompileModePreview CompileMode = "preview"

	CompileModePublish CompileMode = "publish"
)

type DegradeKind string

const (
	DegradeKindBlock DegradeKind = "block"

	DegradeKindTemplate DegradeKind = "template"
)

type Degrade struct {
	Slot string `json:"slot,omitempty"`

	NodeID string `json:"nodeId,omitempty"`

	NodeType string `json:"nodeType,omitempty"`

	Kind DegradeKind `json:"kind"`

	RefID string `json:"refId"`

	Reason string `json:"reason"`
}

func (d Degrade) dedupeKey() string {
	return strings.Join([]string{
		d.Slot, d.NodeID, d.NodeType, string(d.Kind), d.RefID, d.Reason,
	}, "\x00")
}

type DegradeCollector struct {
	items []Degrade
	seen  map[string]bool
}

func NewDegradeCollector() *DegradeCollector {
	return &DegradeCollector{seen: map[string]bool{}}
}

func (c *DegradeCollector) Record(d Degrade) {
	if c == nil {
		return
	}
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	key := d.dedupeKey()
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.items = append(c.items, d)
}

func (c *DegradeCollector) RecordRefFailure(f core.RefFailure) {
	c.Record(Degrade{
		Slot: f.Slot, NodeID: f.NodeID, NodeType: f.NodeType,
		Kind: DegradeKindBlock, RefID: f.BlockID, Reason: f.Reason,
	})
}

func (c *DegradeCollector) Items() []Degrade {
	if c == nil || len(c.items) == 0 {
		return nil
	}
	out := slices.Clone(c.items)
	slices.SortStableFunc(out, func(a, b Degrade) int {
		return cmp.Or(
			cmp.Compare(a.Slot, b.Slot),
			cmp.Compare(a.NodeID, b.NodeID),
			cmp.Compare(a.NodeType, b.NodeType),
			cmp.Compare(string(a.Kind), string(b.Kind)),
			cmp.Compare(a.RefID, b.RefID),
			cmp.Compare(a.Reason, b.Reason),
		)
	})
	return out
}

func refFailureHandler(cfg *compileConfig) func(core.RefFailure) error {
	return func(f core.RefFailure) error {

		cfg.degrade.RecordRefFailure(f)
		if cfg.mode != CompileModePublish || f.Reason == core.RefReasonEmpty {
			return nil
		}
		if f.Slot != "" {
			return fmt.Errorf("结构槽位 %s 绑定的全局块 %s 不可用（%s）：发布被拒绝，避免上线缺结构的不完整页面",
				f.Slot, f.BlockID, f.Reason)
		}
		return fmt.Errorf("节点 %s 引用的全局块 %s 不可用（%s）：发布被拒绝，避免上线缺内容的不完整页面",
			f.NodeID, f.BlockID, f.Reason)
	}
}
