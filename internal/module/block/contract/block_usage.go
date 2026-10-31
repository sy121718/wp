package blockcontract

import (
	"errors"
	"fmt"
	"strings"
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

func (e *BlockInUseError) Error() string {
	if e == nil || len(e.Usages) == 0 {
		return ErrBlockInUse.Error()
	}
	parts := make([]string, 0, len(e.Usages))
	for _, u := range e.Usages {
		parts = append(parts, u.String())
	}
	return fmt.Sprintf("%s: %s", ErrBlockInUse.Error(), strings.Join(parts, "; "))
}

func (e *BlockInUseError) Unwrap() error { return ErrBlockInUse }

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
