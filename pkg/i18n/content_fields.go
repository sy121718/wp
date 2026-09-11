package i18n

// content_fields.go — 字段级内容译文批量注入（多语言 P5d 的实体侧端口，issue #12）。
//
// content.go 只负责「给定原文 + 语境 + 语言，怎么取译文」；本文件补上
// 「一组作者文本值，按构建语言整体替换为译文」这一步，供实体字段解析器
// （商品 / 分类 / 品牌 / 标签 / 属性）复用，避免每个领域模块各写一遍
// 「收集 hash → 构造取词器 → 逐字段替换」的胶水代码。
//
// 语义与 docs/06-D §7.7 完全一致：
//   - 一次批量 SQL（ContentTranslator 的唯一查询形态），渲染期零查库；
//   - 跳过规则命中的值（纯数字 / 纯符号 / 空白）原样返回；
//   - 未命中的值逐字节回退原文，绝不返回空串、绝不报错（决策 F8）。
//
// 语言由调用方传入而不是从上下文取：本包（pkg）不反向依赖 internal/builder，
// 构建语言（core.BuildLang(ctx)）在领域模块侧读出后传进来。
//
// 空 lang（单语言站点 / 未指定构建语言）直接返回，一次查询都不发。

import (
	"context"
	"strings"
)

// TranslateValues 按 lang 批量把 values 里的作者文本替换为译文（就地改写）。
//
// values 的键是字段路径（如 product.name / product_category.name），
// 语境直接取该键 —— 产品域字段路径就是「实体类型.字段名」
// （productcontract.FieldContext 与 core.ContentContext 同一拼法）。
//
// store 为 nil / lang 为空 / 查询失败 → values 原样保留（原文）。
func TranslateValues(ctx context.Context, lang string, store ContentStore, values map[string]string) {
	if len(values) == 0 || store == nil || strings.TrimSpace(lang) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	fields := make([]string, 0, len(values))
	hashes := make([]string, 0, len(values))
	for field, text := range values {
		if !ShouldTranslateContent(text) {
			continue // 纯数字 / 纯符号 / 空白不进翻译表（决策 F7）
		}
		fields = append(fields, field)
		hashes = append(hashes, ContentHash(text))
	}
	if len(hashes) == 0 {
		return
	}

	t := NewContentTranslatorWith(ctx, store, lang, hashes)
	for _, field := range fields {
		values[field] = t.TranslateContent(values[field], field)
	}
}
