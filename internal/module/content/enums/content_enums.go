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

func Err(key string) error { return facing.New(key) }

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

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
