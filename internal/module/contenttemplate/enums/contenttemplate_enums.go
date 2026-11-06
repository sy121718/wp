package contenttemplateenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	ErrDraftVersionConflict = "ErrDraftVersionConflict"
	MsgCreateSuccess        = "MsgCreateSuccess"
	MsgUpdateSuccess        = "MsgUpdateSuccess"
	MsgListSuccess          = "MsgListSuccess"
	MsgDetailSuccess        = "MsgDetailSuccess"

	ErrInvalidParam = "ErrInvalidParam"
	ErrNotFound     = "ErrNotFound"

	ErrTemplateInUse = "ErrTemplateInUse"

	ErrStructureTemplateInUse = "ErrStructureTemplateInUse"
	ErrInvalidType            = "ErrInvalidType"
	ErrDataInvalid            = "ErrDataInvalid"

	ErrFieldBindingInvalid = "ErrFieldBindingInvalid"

	ErrProjectRequired = "ErrProjectRequired"

	ErrProjectNotFound = "ErrProjectNotFound"

	// ErrBulkRejected 是多项删除被整体拒绝时的公共主消息；
	// 单条删除取该条的具体业务原因，不进这句话。
	ErrBulkRejected = "tplBatchReject"

	// ErrReferenceUnavailable 是删除时引用反查端口缺失的事实：
	// 无法确认引用就必须拒绝删除，不静默放行。
	ErrReferenceUnavailable = "tplRefDown"

	// ErrTemplateGone 是写入方发现文档仍引用已删除模板时的拒绝事实。
	ErrTemplateGone = "tplRefGone"
)

func Err(key string) error {
	err := facing.New(key)
	if key == ErrNotFound {
		return facing.WithStatus(err, http.StatusNotFound)
	}
	return err
}

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

const (
	ImpactLoadFailed = "admin.content.templates.impact.loadFailed"

	Heading = "admin.content.templates.heading"
)

func DraftVersionConflict() error { return facing.WithStatus(Err(ErrDraftVersionConflict), 409) }
