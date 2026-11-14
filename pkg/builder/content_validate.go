package builder

import (
	"errors"
	"fmt"
	"go_wp/pkg/richtext"
	"strings"

	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
)

var (
	ErrContentContextInvalid = errors.New("语境非法：必须是已注册组件的可翻译字段（组件类型.字段名）")

	ErrContentSourceSkipped = errors.New("原文不参与翻译（空串、纯数字或纯符号，构建期会跳过）")

	ErrContentTargetEmpty = errors.New("译文不能为空")

	ErrContentTargetTooLong = errors.New("译文超过该字段的长度上限")

	ErrContentTargetShape = errors.New("译文形态与原文不一致：原文含 HTML 标签时译文也必须含标签，原文为纯文本时译文不得含标签")
)

func ContentTargetLimit(contextName string) (limit int, ok bool) {
	typ, field, parsed := i18n.ParseContentContext(contextName)
	if !parsed {
		return 0, false
	}
	meta, whitelisted := core.TranslatableFieldMeta(typ, field)
	if !whitelisted {
		return 0, false
	}
	limit = richtext.MaxRichLen
	if meta.MaxLen > 0 && meta.MaxLen < limit {
		limit = meta.MaxLen
	}
	return limit, true
}

func ValidateContentTarget(contextName, sourceText, targetText string) error {
	typ, field, parsed := i18n.ParseContentContext(contextName)
	if !parsed {
		return fmt.Errorf("%w：%s", ErrContentContextInvalid, strings.TrimSpace(contextName))
	}
	meta, whitelisted := core.TranslatableFieldMeta(typ, field)
	if !whitelisted {
		return fmt.Errorf("%w：%s", ErrContentContextInvalid, contextName)
	}
	if !i18n.ShouldTranslateContent(sourceText) {
		return ErrContentSourceSkipped
	}

	target := strings.TrimSpace(targetText)
	if target == "" {
		return ErrContentTargetEmpty
	}
	if limit, _ := ContentTargetLimit(contextName); limit > 0 && len(target) > limit {
		return fmt.Errorf("%w（上限 %d 字节，实际 %d 字节）", ErrContentTargetTooLong, limit, len(target))
	}
	if richtext.HasRichMarkup(target) != richtext.HasRichMarkup(sourceText) {
		return fmt.Errorf("%w（字段 %s.%s）", ErrContentTargetShape, meta.Type, meta.Field)
	}
	return nil
}
