package builder

// content_validate.go — 译文写入校验（多语言 P5c，docs/06-D §7.8）。
//
// 工作台（后台手动填写）与将来的 AI 译文（§7.9 预留）写入 sys_translation 的行
// 会被构建期直接取用；一旦写坏（语境不在白名单 / 超长 / 形态不符），症状是
// 「产物被静默归一或判空」，比回退原文更糟（§7.6 跳过规则、§7.7 取值逻辑）。
//
// 与构建期同源，不另写一套判断：
//   - 语境白名单   → core.TranslatableFields（P5b 唯一来源）；
//   - 长度与形态   → core.TranslatableFieldMeta（组件 ct tag 声明）+ core.MaxRichLen。
//
// 本文件只做「一条译文能不能写」的纯校验，不碰数据库（写入在 pkg/i18n）。

import (
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

// 校验错误（调用方按 errors.Is 分类；文案可直接展示给编辑者）。
var (
	// ErrContentContextInvalid 语境不是「已注册组件的可翻译字段」。
	ErrContentContextInvalid = errors.New("语境非法：必须是已注册组件的可翻译字段（组件类型.字段名）")
	// ErrContentSourceSkipped 原文不参与翻译（空串 / 纯数字 / 纯符号）。
	ErrContentSourceSkipped = errors.New("原文不参与翻译（空串、纯数字或纯符号，构建期会跳过）")
	// ErrContentTargetEmpty 译文去空白后为空。
	ErrContentTargetEmpty = errors.New("译文不能为空")
	// ErrContentTargetTooLong 译文超过该字段长度上限。
	ErrContentTargetTooLong = errors.New("译文超过该字段的长度上限")
	// ErrContentTargetShape 译文形态与原文不一致（HTML 与纯文本会被构建期归一）。
	ErrContentTargetShape = errors.New("译文形态与原文不一致：原文含 HTML 标签时译文也必须含标签，原文为纯文本时译文不得含标签")
)

// ContentTargetLimit 返回某语境（组件类型.字段名）译文的长度上限（字节）。
//
// 取「控件声明 maxlen」与 core.MaxRichLen 的较小值：
// 富文本字段在构建期由 core.RichTextHTML 处理，超过 MaxRichLen 直接判空；
// 未声明 maxlen 的字段（嵌套数组元素字段）以 MaxRichLen 兜底。
// ok=false 表示语境不在白名单内。
func ContentTargetLimit(contextName string) (limit int, ok bool) {
	typ, field, parsed := i18n.ParseContentContext(contextName)
	if !parsed {
		return 0, false
	}
	meta, whitelisted := core.TranslatableFieldMeta(typ, field)
	if !whitelisted {
		return 0, false
	}
	limit = core.MaxRichLen
	if meta.MaxLen > 0 && meta.MaxLen < limit {
		limit = meta.MaxLen
	}
	return limit, true
}

// ValidateContentTarget 校验一条译文的可写性。
//
// 校验项（全部与构建期同源）：
//  1. 语境在 core.TranslatableFields 白名单内（未声明字段永不翻译，§7.5）；
//  2. 原文参与翻译（i18n.ShouldTranslateContent，§7.6 跳过规则）；
//  3. 译文去空白后非空（表上有 CHECK 约束：target_text 不得为空串）；
//  4. 译文长度 ≤ 字段上限（ContentTargetLimit）；
//  5. 译文形态与原文一致：是否含 HTML 标签必须相同（core.HasRichMarkup）——
//     原文是 HTML、译文是纯文本时构建期会把译文转义后包 <p>（形态丢失）；
//     反之原文纯文本、译文带标签时标签会以文本形式出现在产物里。
//
// source_hash 一致性不在本函数（写入层 pkg/i18n 负责重算校验）。
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
	if core.HasRichMarkup(target) != core.HasRichMarkup(sourceText) {
		return fmt.Errorf("%w（字段 %s.%s）", ErrContentTargetShape, meta.Type, meta.Field)
	}
	return nil
}
