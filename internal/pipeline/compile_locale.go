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
	out = opts
	if !ContentTranslationEnabled(ctx, project, projectID, lang) || page == nil || makeTranslator == nil {
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
