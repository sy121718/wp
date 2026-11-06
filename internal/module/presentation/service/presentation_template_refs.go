package presentationservice

import (
	"context"
	"sort"

	"gorm.io/gorm"

	presentationcontract "go_wp/internal/module/presentation/contract"
	"go_wp/internal/pipeline"
)

var _ presentationcontract.TemplateRefTxReader = (*Service)(nil)

// ListInstanceTemplateRefsTx 在调用方事务内反查实例绑定、独立文档与快照版本外键。
func (s *Service) ListInstanceTemplateRefsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs, versionIDs []string) ([]presentationcontract.TemplateRef, error) {
	rows, err := s.m.ListInstanceTemplateRefRowsTx(ctx, tx, projectID, templateIDs, versionIDs)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(templateIDs))
	for _, id := range templateIDs {
		known[id] = true
	}
	out := make([]presentationcontract.TemplateRef, 0, len(rows))
	for _, row := range rows {
		if row.FromSnapshot {
			// 快照外键命中版本：模板 id 由版本归属决定，调用方按 versionIDs 一并交付，
			// 这里用空 template_id 表示「按版本命中」，reason 仍落在同一批目标上。
			for _, id := range templateIDs {
				out = append(out, presentationcontract.TemplateRef{
					TemplateID: id, InstanceID: row.InstanceID, EntityType: row.EntityType,
					EntityID: row.EntityID, URLPath: row.URLPath,
				})
			}
			continue
		}
		slotsByTemplate := map[string][]string{}
		if len(row.Document) > 0 {
			if bindings, perr := pipeline.ParseStructureBindings(row.Document); perr == nil {
				for slot, id := range bindings.TemplateBindings() {
					if known[id] {
						slotsByTemplate[id] = append(slotsByTemplate[id], slot)
					}
				}
			}
		}
		if len(slotsByTemplate) == 0 && row.TemplateID != "" {
			slotsByTemplate[row.TemplateID] = nil
		}
		for id, slots := range slotsByTemplate {
			sort.Strings(slots)
			out = append(out, presentationcontract.TemplateRef{
				TemplateID: id, InstanceID: row.InstanceID, EntityType: row.EntityType,
				EntityID: row.EntityID, URLPath: row.URLPath, Slots: slots,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TemplateID != out[j].TemplateID {
			return out[i].TemplateID < out[j].TemplateID
		}
		return out[i].InstanceID < out[j].InstanceID
	})
	return out, nil
}
