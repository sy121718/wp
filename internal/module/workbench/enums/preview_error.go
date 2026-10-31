package workbenchenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

func PreviewError(key string) error {
	switch key {
	case ErrBlockNotFound:
		return facing.WithStatus(Err(key), http.StatusNotFound)
	case ErrDraftVersionStale:
		return facing.WithStatus(Err(key), http.StatusConflict)
	default:
		return facing.WithStatus(Err(key), http.StatusBadRequest)
	}
}
