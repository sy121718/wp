package pipeline

// compile_locale.go — 语言与内容翻译装配（page / presentation 共用，EDT-003 基础层）。

import (
	"context"
	"strings"

	"go_wp/internal/builder"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

// LocaleCompileOptions 构建语言 + 构建期冻结词条快照（P4，docs/06-D §2.3）。
func LocaleCompileOptions(lang string) []builder.CompileOption {
	return []builder.CompileOption{
		builder.WithLanguage(lang),
		builder.WithTranslator(i18n.Snapshot(lang)),
	}
}

// ContentTranslatorFactory 构造内容译文取词器（page 可注入存储，presentation 走默认存储）。
//
// projectID 为本次构建的站点工程（审计 I18N-009）：取词按「本工程行优先、未命中回落
// 全局行」；空值表示调用方没有工程上下文（等价于接入工程作用域之前的行为）。
type ContentTranslatorFactory func(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator

// AppendContentTranslation 非默认语言时收集候选并追加 WithContentTranslator（P5b）。
//
// 返回追加后的 opts、取词器实例（可能 nil）与候选数（供 L3 缺失告警）。
func AppendContentTranslation(
	opts []builder.CompileOption,
	ctx context.Context,
	project projectcontract.ProjectService,
	projectID, lang string,
	page *builder.Page,
	resolve builder.BlockRootFunc,
	makeTranslator ContentTranslatorFactory,
) (out []builder.CompileOption, translator *i18n.ContentTranslator, candidates int) {
	return AppendContentTranslationFor(opts, ctx, projectID,
		DefaultLocale(ctx, project, projectID), lang, page, resolve, makeTranslator)
}

// AppendContentTranslationFor 用**给定**默认语言接入内容翻译（审计 I18N-01 冻结口径）。
//
// 与 AppendContentTranslation 的差别只有默认语言的来源：发布 / 重建路径的默认语言
// 来自发布计划（冻结值），现场解析会让「改了 is_default」把既有语言的产物语义整个
// 翻转 —— 原来的默认语言产物（原文直出）突然开始查译文表，字节随之改变，
// 而线上路径、语言集合一个都没动。
func AppendContentTranslationFor(
	opts []builder.CompileOption,
	ctx context.Context,
	projectID, defaultLang, lang string,
	page *builder.Page,
	resolve builder.BlockRootFunc,
	makeTranslator ContentTranslatorFactory,
) (out []builder.CompileOption, translator *i18n.ContentTranslator, candidates int) {
	out = opts
	if !ContentTranslationEnabledFor(defaultLang, lang) || page == nil || makeTranslator == nil {
		return out, nil, 0
	}
	cands := builder.CollectContentCandidatesForDocument(page, resolve)
	if len(cands) == 0 {
		return out, nil, 0
	}
	candidates = len(cands)
	translator = makeTranslator(ctx, projectID, lang, builder.ContentHashes(cands))
	// 缺译统计进 Manifest（审计 I18N-02）：候选数与取词器引用交回内核，内核在
	// 编译结束后读一次缺失数并写进 Manifest。日志（LogContentTranslationMisses）
	// 只作人工排查用 —— 它滚走了、也没法在发布验收里被机器判定。
	//
	// 缺失数此刻必须**不能**取：这时候还没渲染，取到的恒为 0，写进 Manifest
	// 就是一份「零缺失」的假事实 —— 比不记更坏。
	CompileUsageFromContext(ctx).RecordContentTranslation(candidates, translator)
	return append(out, builder.WithContentTranslator(translator)), translator, candidates
}

// HighlightPath 导航「当前项」高亮路径：逻辑路径 → 本语言访问路径（默认语言现场解析）。
func HighlightPath(ctx context.Context, project projectcontract.ProjectService, projectID, lang, logical string) string {
	return HighlightPathWithDefault(ctx, project, projectID, DefaultLocale(ctx, project, projectID), lang, logical)
}

// HighlightPathWithDefault 用**给定**默认语言计算高亮路径（审计 I18N-01 冻结口径）。
//
// 高亮路径进产物字节（导航当前项），而它经 LangURLRule 映射 —— 规则里的默认语言
// 决定哪个语言不带前缀。冻结输入时若这里回读 is_default，重建就会换掉「当前项」标记。
func HighlightPathWithDefault(ctx context.Context, project projectcontract.ProjectService, projectID, defaultLang, lang, logical string) string {
	if strings.TrimSpace(logical) == "" {
		return ""
	}
	p, err := SitePath(LangURLRuleForProjectWithDefault(ctx, project, projectID, defaultLang), lang, logical)
	if err != nil {
		return ""
	}
	return p
}
