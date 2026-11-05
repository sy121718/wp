package blockcontract

import (
	"errors"
	"strings"

	"go_wp/pkg/facing"
)

type BlockUsageKind string

const (
	UsageKindPageDocument BlockUsageKind = "page_document"

	UsageKindPageStructure BlockUsageKind = "page_structure"

	UsageKindThemeSlot BlockUsageKind = "theme_slot"

	UsageKindBlockDocument BlockUsageKind = "block_document"

	UsageKindPageRevision BlockUsageKind = "page_revision"

	UsageKindContentTemplate BlockUsageKind = "content_template"

	UsageKindPresentationInstance BlockUsageKind = "presentation_instance"
)

type BlockUsage struct {
	Kind BlockUsageKind

	ProjectID string

	EntityID string

	Label string

	Detail string
}

func (u BlockUsage) String() string {
	var b strings.Builder
	b.WriteString(string(u.Kind))
	if label := strings.TrimSpace(u.Label); label != "" {
		b.WriteString(" ")
		b.WriteString(label)
	} else if id := strings.TrimSpace(u.EntityID); id != "" {
		b.WriteString(" ")
		b.WriteString(id)
	}
	if detail := strings.TrimSpace(u.Detail); detail != "" {
		b.WriteString("(")
		b.WriteString(detail)
		b.WriteString(")")
	}
	return b.String()
}

type BlockInUseError struct {
	Usages []BlockUsage
}

func blockUsageKindKey(kind BlockUsageKind) string {
	switch kind {
	case UsageKindPageDocument:
		return "MsgBlockUsagePageDocument"
	case UsageKindPageStructure:
		return "MsgBlockUsagePageStructure"
	case UsageKindPageRevision:
		return "MsgBlockUsagePageRevision"
	case UsageKindThemeSlot:
		return "MsgBlockUsageThemeSlot"
	case UsageKindBlockDocument:
		return "MsgBlockUsageBlockDocument"
	case UsageKindContentTemplate:
		return "MsgBlockUsageContentTemplate"
	case UsageKindPresentationInstance:
		return "MsgBlockUsagePresentationInstance"
	default:
		return string(kind)
	}
}

// Error 只给业务 key：引用明细是界面事实，走 BlockedItems 由失败片段逐条取词。
// 把明细拼进 Error() 会让它经日志与错误链外泄内部枚举（page_document 等）。
func (e *BlockInUseError) Error() string { return ErrBlockInUse.Error() }

func (e *BlockInUseError) Unwrap() error { return ErrBlockInUse }

// Facing 只交业务 key：引用明细走 BlockedItems，由失败片段逐条取词，
// 不让 err.Error() 里的内部枚举原文混进对外提示。
func (e *BlockInUseError) Facing() (string, string) { return ErrBlockInUse.Error(), "" }

// BlockedItems 把引用位置交失败的 HTML 出口逐条取词，
// 不让控制器把「类型 + 位置」拼成一条成品句。
func (e *BlockInUseError) BlockedItems() []facing.BlockedItem {
	if e == nil {
		return nil
	}
	items := make([]facing.BlockedItem, 0, len(e.Usages))
	for _, u := range e.Usages {
		name := strings.TrimSpace(u.Label)
		if name == "" {
			name = strings.TrimSpace(u.EntityID)
		}
		items = append(items, facing.BlockedItem{
			ID:        u.EntityID,
			Name:      name,
			ReasonKey: blockUsageKindKey(u.Kind),
			Fallback:  strings.TrimSpace(u.Detail),
		})
	}
	return items
}

func NewBlockInUseError(usages []BlockUsage) error {
	if len(usages) == 0 {
		return ErrBlockInUse
	}
	return &BlockInUseError{Usages: usages}
}

func BlockUsages(err error) []BlockUsage {
	var inUse *BlockInUseError
	if !errors.As(err, &inUse) {
		return nil
	}
	return inUse.Usages
}
