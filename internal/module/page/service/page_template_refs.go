package pageservice

import (
	"context"
	"sort"

	"gorm.io/gorm"

	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/internal/pipeline"
)

var _ pagecontract.TemplateRefTxReader = (*Service)(nil)

// ListPageTemplateRefsTx 在调用方事务内反查页面草稿、历史版本与现行产物依赖。
func (s *Service) ListPageTemplateRefsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs []string) ([]pagecontract.TemplateRef, error) {
	rows, err := s.model.ListTemplateRefRowsTx(ctx, tx, projectID, templateIDs)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(templateIDs))
	for _, id := range templateIDs {
		known[id] = true
	}
	out := make([]pagecontract.TemplateRef, 0, len(rows))
	for _, row := range rows {
		if row.FromDependency {
			// 依赖行只证明「现行产物仍声明依赖」；槽位由草稿/历史行给出。
			for _, id := range templateIDs {
				out = append(out, pagecontract.TemplateRef{
					TemplateID: id, PageID: row.PageID, Title: row.Title, Path: row.DraftPath,
				})
			}
			continue
		}
		bindings, perr := pipeline.ParseStructureBindings(row.Document)
		if perr != nil {
			for _, id := range templateIDs {
				out = append(out, pagecontract.TemplateRef{
					TemplateID: id, PageID: row.PageID, Title: row.Title, Path: row.DraftPath,
				})
			}
			continue
		}
		slotsByTemplate := map[string][]string{}
		for slot, id := range bindings.TemplateBindings() {
			if known[id] {
				slotsByTemplate[id] = append(slotsByTemplate[id], slot)
			}
		}
		for id, slots := range slotsByTemplate {
			sort.Strings(slots)
			out = append(out, pagecontract.TemplateRef{
				TemplateID: id, PageID: row.PageID, Title: row.Title, Path: row.DraftPath, Slots: slots,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TemplateID != out[j].TemplateID {
			return out[i].TemplateID < out[j].TemplateID
		}
		return out[i].PageID < out[j].PageID
	})
	return out, nil
}
