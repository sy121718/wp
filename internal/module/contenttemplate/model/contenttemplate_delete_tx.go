package contenttemplatemodel

import (
	"context"
	"slices"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// TransactionAny 提供一个不带工程作用域的事务：跨工程批量删除在同一事务里
// 完成，工程作用域由调用方按稳定顺序逐段切换。
func (m *Model) TransactionAny(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ScopeTx 在调用方事务内切换到指定工程作用域。
func (m *Model) ScopeTx(tx *gorm.DB, projectID string) error {
	return rls.ScopeTx(tx, projectID)
}

// ListByIDs 逐工程定位模板（不进事务、不加锁），用于批量删除前的归属探测。
func (m *Model) ListByIDs(ctx context.Context, projectID string, ids []string) (rows []TemplateEntity, err error) {
	if strings.TrimSpace(projectID) == "" || len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&TemplateEntity{}).
			Where("project_id = ? AND id IN ?", projectID, ids).
			Order("id ASC").Find(&rows).Error
	})
	return rows, err
}

// ListByIDsTx 在调用方事务内按 id 查询尚未加锁的模板行（写者做存在性复核用）。
func (m *Model) ListByIDsTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) (rows []TemplateEntity, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	err = tx.WithContext(ctx).Model(&TemplateEntity{}).
		Where("project_id = ? AND id IN ?", projectID, ids).
		Order("id ASC").Find(&rows).Error
	return rows, err
}

// MissingTemplateIDsTx 在调用方事务内返回这批模板里不存在的 id。
func (m *Model) MissingTemplateIDsTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) ([]string, error) {
	rows, err := m.ListByIDsTx(ctx, tx, projectID, ids)
	if err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(rows))
	for i := range rows {
		found[rows[i].ID] = true
	}
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// ExistingVersionIDsTx 在调用方事务内返回确实存在的模板版本 id。
func (m *Model) ExistingVersionIDsTx(ctx context.Context, tx *gorm.DB, projectID string, versionIDs []string) ([]string, error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	if len(versionIDs) == 0 {
		return nil, nil
	}
	var out []string
	err := tx.WithContext(ctx).
		Table(tableNameContentTemplateVersions+" AS v").
		Joins("JOIN "+tableNameContentTemplates+" AS t ON t.id = v.template_id").
		Where("t.project_id = ? AND v.id IN ?", projectID, versionIDs).
		Order("v.id ASC").Pluck("v.id", &out).Error
	return out, err
}

// LockByIDsTx 在已切换到工程作用域的事务内按 id 升序锁定模板行。
func (m *Model) LockByIDsTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) (rows []TemplateEntity, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	err = tx.WithContext(ctx).Model(&TemplateEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("project_id = ? AND id IN ?", projectID, sorted).
		Order("id ASC").Find(&rows).Error
	return rows, err
}

// VersionIDsOfTx 返回这批模板的全部历史版本 id；自动发布快照直接外键到版本，
// 删除保护必须把它们一并交给引用反查端口。
func (m *Model) VersionIDsOfTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) ([]string, error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var out []string
	err := tx.WithContext(ctx).
		Table(tableNameContentTemplateVersions+" AS v").
		Joins("JOIN "+tableNameContentTemplates+" AS t ON t.id = v.template_id").
		Where("t.project_id = ? AND v.template_id IN ?", projectID, ids).
		Order("v.id ASC").Pluck("v.id", &out).Error
	return out, err
}

// StructureBindingRefsTx 在事务内反查「其它模板文档仍把目标绑定为结构槽」的事实。
func (m *Model) StructureBindingRefsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs []string) (rows []StructureRefRow, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	if len(templateIDs) == 0 {
		return nil, nil
	}
	list := strings.Join(templateIDs, ",")
	err = tx.WithContext(ctx).Table(tableNameContentTemplates).
		Select("id, name, draft_document").
		Where("project_id = ? AND NOT (id::text = ANY(string_to_array(?, ',')))", projectID, list).
		Where(`draft_document->'settings'->'structure'->>'headerTemplateId' = ANY(string_to_array(?, ','))
			OR draft_document->'settings'->'structure'->>'footerTemplateId' = ANY(string_to_array(?, ','))
			OR (jsonb_typeof(draft_document->'settings'->'structure'->'slotTemplates') = 'object'
			    AND EXISTS (SELECT 1 FROM jsonb_each_text(draft_document->'settings'->'structure'->'slotTemplates') AS kv(slot, tid)
			                WHERE kv.tid = ANY(string_to_array(?, ','))))`,
			list, list, list).
		Order("id ASC").Scan(&rows).Error
	return rows, err
}

// DeleteTemplateTx 删除单个模板的 pins、版本与实体行，并核对实体行的受影响行数。
//
// 调用方已在同一事务内锁住该行并完成引用复核；实体行没删到就是并发缺陷，必须回滚。
func (m *Model) DeleteTemplateTx(ctx context.Context, tx *gorm.DB, projectID, id string) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Table(tableNameContentTemplatePins).
		Where("template_id = ?", id).Delete(nil).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Where("template_id = ?", id).Delete(&VersionEntity{}).Error; err != nil {
		return err
	}
	res := tx.WithContext(ctx).Model(&TemplateEntity{}).
		Where("id = ? AND project_id = ?", id, projectID).Delete(&TemplateEntity{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SortedProjectIDs 返回去空白、去重后的工程 id，按字典序稳定排列，
// 供批量删除按同一个顺序取工程内容写锁与切换 RLS 作用域。
func SortedProjectIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}
