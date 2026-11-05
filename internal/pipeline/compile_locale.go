package pipeline

import (
	"context"
	"strings"

	"go_wp/pkg/builder"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

func LocaleCompileOptions(lang string) []builder.CompileOption {
	return []builder.CompileOption{
		builder.WithLanguage(lang),
		builder.WithTranslator(i18n.Snapshot(lang)),
	}
}

type ContentTranslatorFactory func(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator

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

	CompileUsageFromContext(ctx).RecordContentTranslation(candidates, translator)
	return append(out, builder.WithContentTranslator(translator)), translator, candidates
}

func HighlightPath(ctx context.Context, project projectcontract.ProjectService, projectID, lang, logical string) string {
	return HighlightPathWithDefault(ctx, project, projectID, DefaultLocale(ctx, project, projectID), lang, logical)
}

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
