package pageservice

import (
	"context"

	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/builder"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/i18n"
)

// SetTemplateExistencePort 注入模板存在性复核端口。
//
// 页面文档新增结构模板绑定时，写事务必须持工程内容写锁并确认这些模板仍存在；
// 端口缺失时只要文档引用了结构模板就拒绝写入（fail-closed）。
func (s *Service) SetTemplateExistencePort(port contenttemplatecontract.TemplateExistenceTxReader) {
	s.templateCheck = port
}

// templateIDsInPageDocument 返回页面文档 settings.structure 里绑定的模板 id。
func templateIDsInPageDocument(doc json.RawMessage) []string {
	if len(doc) == 0 {
		return nil
	}
	bindings, err := pipeline.ParseStructureBindings(doc)
	if err != nil {
		return nil
	}
	return builderTemplateIDs(bindings)
}

func builderTemplateIDs(bindings builder.StructureBindings) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(bindings.SlotTemplates)+2)
	for _, id := range bindings.TemplateBindings() {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// lockAndCheckPageTemplateRefsTx 在写事务内先取工程内容写锁，再确认文档引用的
// 结构模板仍存在：引用新增与模板删除共用同一把锁，删除提交后写者不会写出悬空引用。
func (s *Service) lockAndCheckPageTemplateRefsTx(ctx context.Context, tx *gorm.DB, projectID string, doc json.RawMessage) error {
	ids := templateIDsInPageDocument(doc)
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
