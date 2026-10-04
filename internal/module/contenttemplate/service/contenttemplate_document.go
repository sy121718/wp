package contenttemplateservice

// contenttemplate_document.go — 模板文档的校验与哈希（版本快照的判据）。
//
// 两个口径：保存草稿走宽容校验（ValidatePageTolerant），发布/解析走严格校验（ValidatePage）；
// 两者都带字段绑定白名单校验（越界绑定在保存时即拒绝），结构模板（页眉/页脚）另行
// **明确拒绝**字段绑定 —— 它们在构建期按引用页面的实体解析，不是"全局结构"应有的行为。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"go_wp/internal/builder"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

// hashDocument 版本文档内容哈希（content_template_versions.source_hash）。
func hashDocument(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

// validateDocument 草稿保存：ValidatePageTolerant + 字段绑定白名单（EDT-013）。
func (s *Service) validateDocument(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	return s.validateDocumentMode(entityType, raw, true)
}

// validateDocumentStrict 发布/解析口径：完整 ValidatePage，非法模板在取用前拒绝。
func (s *Service) validateDocumentStrict(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	return s.validateDocumentMode(entityType, raw, false)
}

func (s *Service) validateDocumentMode(entityType string, raw json.RawMessage, tolerant bool) (json.RawMessage, error) {
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if tolerant {
		if _, err = builder.ValidatePageTolerant(page); err != nil {
			return nil, errors.New(contenttemplateenums.ErrDataInvalid)
		}
	} else if err = builder.ValidatePage(page); err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if contenttemplatemodel.IsStructureTemplateType(entityType) {
		// 结构模板（页眉 / 页脚）不是内容实体：字段绑定在构建期会按**引用它的那个页面**
		// 的实体上下文解析 —— 同一份页眉在商品页显示商品名、在文章页显示标题，
		// 那不是"全局结构"应有的行为。故这里**明确拒绝**，而不是跳过校验
		//（跳过要等线上才看得出页眉串了数据）。
		refs, rerr := builder.CollectFieldRefs(page)
		if rerr != nil {
			return nil, errors.New(contenttemplateenums.ErrDataInvalid)
		}
		if len(refs) > 0 {
			return nil, errors.New(contenttemplateenums.ErrFieldBindingInvalid)
		}
	} else if err = builder.ValidateFieldRefs(page, entityType, s.registry); err != nil {
		return nil, fmt.Errorf("%s: %w", contenttemplateenums.ErrFieldBindingInvalid, err)
	}
	doc, err := json.Marshal(page)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	return doc, nil
}
