package navigationenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

const ErrSourceKindFailed = "navigation.err.source.kindFailed"

const sourceKindStatus = http.StatusInternalServerError

func ErrSourceKind() error {
	return facing.WithStatus(facing.New(ErrSourceKindFailed), sourceKindStatus)
}

func ErrSourceKindDetail(detail string) error {
	return facing.WithStatus(ErrDetail(ErrSourceKindFailed, detail), sourceKindStatus)
}
