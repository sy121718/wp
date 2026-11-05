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
