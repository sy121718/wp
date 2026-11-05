package presentationenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	ErrActionTargetInUse = "presentation.err.actionTargetInUse"
	MsgCreateSuccess     = "MsgCreateSuccess"
	MsgRebuildSuccess    = "MsgRebuildSuccess"
	MsgListSuccess       = "MsgListSuccess"
	MsgDetailSuccess     = "MsgDetailSuccess"
	MsgDeleteSuccess     = "MsgDeleteSuccess"

	MsgPreviewSuccess = "MsgPreviewSuccess"

	MsgUpdateURLSuccess = "MsgUpdateURLSuccess"

	ErrInvalidParam  = "ErrInvalidParam"
	ErrNotFound      = "ErrNotFound"
	ErrNoTemplate    = "ErrNoTemplate"
	ErrEntityMissing = "ErrEntityMissing"
	ErrBuildFailed   = "ErrBuildFailed"

	ErrProjectRequired = "ErrProjectRequired"

	ErrProjectNotFound = "ErrProjectNotFound"

	ErrRegistryMissing = "ErrRegistryMissing"

	ErrTemplateTypeMismatch = "ErrTemplateTypeMismatch"

	ErrInvalidPath = "ErrInvalidPath"

	ErrSamePath = "ErrSamePath"

	ErrPathOccupied = "ErrPathOccupied"

	ErrDetachConfirmRequired = "presentation.err.detachConfirmRequired"

	ErrRollbackTargetMiss = "presentation.err.rollbackTargetMiss"

	ErrRollbackFailed = "presentation.err.rollbackFailed"

	ErrSnapshotMismatch = "presentation.err.snapshotMismatch"

	ErrInputMoved = "presentation.err.inputMoved"
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
	StatusActive = "active"

	StatusDraft = "draft"
)

func ActionTargetInUse() error { return facing.WithStatus(Err(ErrActionTargetInUse), 409) }

func InputMoved() error { return facing.WithStatus(Err(ErrInputMoved), http.StatusConflict) }
