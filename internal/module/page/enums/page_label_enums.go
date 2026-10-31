package pageenums

import (
	"strconv"
	"strings"
)

const (
	RebuildStagePlan = "plan"

	RebuildStageBuild = "build"
)

func RebuildStageLabel(stage string) (key, fallback string) {
	switch stage {
	case RebuildStagePlan:
		return "admin.pages.impact.stage_plan", "计划阶段（站点语言清单或旧发布范围读不到）"
	case RebuildStageBuild:
		return "admin.pages.impact.stage_build", "构建 / 发布阶段"
	default:
		return "", stage
	}
}

const (
	LangStatusExcluded  = "excluded"
	LangStatusPublished = "published"
	LangStatusDraft     = "draft"
)

func LangStatusOf(excluded, published bool) string {
	switch {
	case excluded:
		return LangStatusExcluded
	case published:
		return LangStatusPublished
	default:
		return LangStatusDraft
	}
}

func LangStatusParse(raw, fallback string) string {
	switch strings.TrimSpace(raw) {
	case LangStatusExcluded, LangStatusPublished, LangStatusDraft:
		return strings.TrimSpace(raw)
	default:
		return fallback
	}
}

func LangStatusLabel(status string) (key, fallback string) {
	switch status {
	case LangStatusExcluded:
		return "admin.page.langs.status.excluded", "已排除（不产出）"
	case LangStatusPublished:
		return "admin.page.langs.status.published", "已发布"
	default:
		return "admin.page.langs.status.draft", "未发布"
	}
}

const (
	RedirectResultCreated = "created"
	RedirectResultDeleted = "deleted"
	RedirectResultMerged  = "merged"
)

func RedirectResultLabel(code string) (key, fallback string) {
	switch code {
	case RedirectResultCreated:
		return "admin.redirect.ok.created", "重定向已新增。"
	case RedirectResultDeleted:
		return "admin.redirect.ok.deleted", "重定向已删除。"
	case RedirectResultMerged:
		return "admin.redirect.ok.merged", "重定向链已合并为直达。"
	default:
		return "", code
	}
}

const (
	TranslationOriginHeader = "header"

	TranslationOriginFooter = "footer"

	TranslationOriginBlock = "block"
)

const (
	slotKeyHeader = "header"
	slotKeyFooter = "footer"
)

func TranslationOriginOfSlot(slot string) string {
	switch slot {
	case slotKeyHeader:
		return TranslationOriginHeader
	case slotKeyFooter:
		return TranslationOriginFooter
	default:
		return TranslationOriginBlock
	}
}

type Outcome struct {
	Key string

	Fallback string

	Args []string
}

func PagesBulkDeleteOutcome(deleted, skipped int) Outcome {
	switch {
	case deleted == 0 && skipped == 0:
		return Outcome{Key: BulkPageNoneSelected, Fallback: "没有勾选任何页面，列表未改动。"}
	case skipped == 0:
		return Outcome{Key: BulkPageAllDeleted, Fallback: "已删除 %s 个页面。", Args: []string{itoa(deleted)}}
	case deleted == 0:
		return Outcome{Key: BulkPageAllSkipped, Fallback: "%s 个页面都未能删除，列表未改动。", Args: []string{itoa(skipped)}}
	default:
		return Outcome{Key: BulkPagePartial, Fallback: "已删除 %s 个，%s 个未能删除（可能已被删除或路径清理失败）。",
			Args: []string{itoa(deleted), itoa(skipped)}}
	}
}

func RedirectBulkDeleteOutcome(deleted, skipped int) Outcome {
	switch {
	case deleted == 0 && skipped == 0:
		return Outcome{Key: BulkRedirectNoneSelected, Fallback: "没有勾选任何重定向，列表未改动。"}
	case skipped == 0:
		return Outcome{Key: BulkRedirectAllDeleted, Fallback: "已删除 %s 条重定向。", Args: []string{itoa(deleted)}}
	case deleted == 0:
		return Outcome{Key: BulkRedirectAllSkipped, Fallback: "%s 条重定向都未能删除，列表未改动。", Args: []string{itoa(skipped)}}
	default:
		return Outcome{Key: BulkRedirectPartial, Fallback: "已删除 %s 条，%s 条未能删除（可能已不存在或访问面不可用）。",
			Args: []string{itoa(deleted), itoa(skipped)}}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
