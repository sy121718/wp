package projectservice

import (
	"context"
	"sort"

	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/i18n"
)

var _ projectcontract.TemplateRefTxReader = (*Service)(nil)

// ListThemeTemplateRefsTx 在调用方事务内反查主题设置的结构模板绑定。
func (s *Service) ListThemeTemplateRefsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs []string) ([]projectcontract.TemplateRef, error) {
	rows, err := s.model.ListThemeTemplateRefRowsTx(ctx, tx, projectID, templateIDs)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(templateIDs))
	for _, id := range templateIDs {
		known[id] = true
	}
	out := make([]projectcontract.TemplateRef, 0, len(rows))
	for _, row := range rows {
		bindings, perr := pipeline.ParseStructureBindings(row.Settings)
		if perr != nil {
			for _, id := range templateIDs {
				out = append(out, projectcontract.TemplateRef{
					TemplateID: id, ThemeID: row.ThemeID, ThemeName: row.ThemeName,
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
			out = append(out, projectcontract.TemplateRef{
				TemplateID: id, ThemeID: row.ThemeID, ThemeName: row.ThemeName, Slots: slots,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TemplateID != out[j].TemplateID {
			return out[i].TemplateID < out[j].TemplateID
		}
		return out[i].ThemeID < out[j].ThemeID
	})
	return out, nil
}

// SetTemplateExistencePort 注入模板存在性复核端口。
//
// 主题设置保存结构模板绑定时，写事务持同一工程内容写锁并确认模板仍存在；
// 端口缺失时只要设置里引用了结构模板就拒绝写入（fail-closed）。
func (s *Service) SetTemplateExistencePort(port contenttemplatecontract.TemplateExistenceTxReader) {
	s.templateCheck = port
}

// lockAndCheckThemeRefsTx 在主题写事务内先取工程内容写锁，再确认新引用的结构模板仍存在。
func (s *Service) lockAndCheckThemeRefsTx(ctx context.Context, tx *gorm.DB, projectID string, settings json.RawMessage) error {
	ids := templateIDsInThemeSettings(settings)
	if len(ids) == 0 {
		return nil
	}
	if err := i18n.LockProjectContentWrite(ctx, tx, projectID); err != nil {
		return err
	}
	if s.templateCheck == nil {
		return contenttemplatecontract.ErrTemplateGone()
	}
	missing, err := s.templateCheck.MissingTemplateIDsTx(ctx, tx, projectID, ids)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return contenttemplatecontract.ErrTemplateGone()
	}
	return nil
}

// templateIDsInThemeSettings 返回主题设置里的结构模板绑定 id。
func templateIDsInThemeSettings(settings json.RawMessage) []string {
	if len(settings) == 0 {
		return nil
	}
	bindings, err := pipeline.ParseStructureBindings(settings)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(bindings.SlotTemplates)+2)
	seen := map[string]bool{}
	for _, id := range bindings.TemplateBindings() {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}
