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
type ContentTranslatorFactory func(ctx context.Context, lang string, hashes []string) *i18n.ContentTranslator

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
	out = opts
	if !ContentTranslationEnabled(ctx, project, projectID, lang) || page == nil || makeTranslator == nil {
		return out, nil, 0
	}
	cands := builder.CollectContentCandidatesForDocument(page, resolve)
	if len(cands) == 0 {
		return out, nil, 0
	}
	candidates = len(cands)
	translator = makeTranslator(ctx, lang, builder.ContentHashes(cands))
	return append(out, builder.WithContentTranslator(translator)), translator, candidates
}

// HighlightPath 导航「当前项」高亮路径：逻辑路径 → 本语言访问路径。
func HighlightPath(ctx context.Context, project projectcontract.ProjectService, projectID, lang, logical string) string {
	if strings.TrimSpace(logical) == "" {
		return ""
	}
	p, err := SitePath(LangURLRuleForProject(ctx, project, projectID), lang, logical)
	if err != nil {
		return ""
	}
	return p
}
