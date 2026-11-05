package pubenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

const (
	ErrRouteOccupied  = "ErrRouteOccupied"
	ErrRouteNotFound  = "ErrRouteNotFound"
	ErrReceiptPending = "ErrReceiptPending"

	ErrInvalidParam = "ErrInvalidParam"

	ErrProjectRequired = "ErrProjectRequired"

	ErrRouteActiveRename = "ErrRouteActiveRename"

	ErrInternal = "ErrInternal"
)

const (
	MsgPublished   = "MsgPublished"
	MsgRolledBack  = "MsgRolledBack"
	MsgDeactivated = "MsgDeactivated"
)

func Err(key string) error { return facing.New(key) }

func ErrProjectMissing() error {
	return facing.WithStatus(Err(ErrProjectRequired), http.StatusBadRequest)
}
