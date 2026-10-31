package contentenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"

	MsgCollectionsSuccess = "MsgCollectionsSuccess"

	ErrInvalidParam = "ErrInvalidParam"
	ErrNotFound     = "ErrNotFound"
	ErrInvalidType  = "ErrInvalidType"
	ErrInvalidField = "ErrInvalidField"
	ErrSlugTaken    = "ErrSlugTaken"
	ErrDataInvalid  = "ErrDataInvalid"

	ErrCollectionUnsupported = "ErrCollectionUnsupported"
)

var errFallbacks = map[string]string{
	ErrMissingID:             "缺少文章标识，请回到列表页重试。",
	ErrMissingTitle:          "请先填写文章标题。",
	ErrMissingSlug:           "请先填写文章路径（slug）。",
	ErrInvalidParam:          "提交的信息不完整，请检查后重试。",
	ErrNotFound:              "这篇文章不存在，可能已被删除。",
	ErrInvalidType:           "内容类型不受支持。",
	ErrInvalidField:          "提交了不支持的字段，请刷新页面后重试。",
	ErrSlugTaken:             "这个路径（slug）已被另一条内容占用，换一个。",
	ErrDataInvalid:           "内容数据格式异常，请联系管理员。",
	ErrCollectionUnsupported: "当前内容服务未实现集合元数据契约。",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

func FallbackFor(key string) string { return errFallbacks[key] }

const (
	BulkArticleNoneSelected = "content.bulk.noneSelected"
	BulkArticleAllDeleted   = "content.bulk.allDeleted"
	BulkArticleAllSkipped   = "content.bulk.allSkipped"
	BulkArticlePartial      = "content.bulk.partial"
)

const (
	StatePublished   = "admin.article.state.published"
	StateUnpublished = "admin.article.state.unpublished"

	ImportHintSaveFirst = "admin.article.import.hint.saveFirst"
	ImportHintNoProject = "admin.article.import.hint.noProject"

	ScoreSerpTitleEmpty = "admin.article.score.serpTitleEmpty"
	ScoreSerpDescEmpty  = "admin.article.score.serpDescEmpty"
)
