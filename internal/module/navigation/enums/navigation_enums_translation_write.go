package navigationenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

const (
	ErrTranslationDuplicate = "navigation.err.translation.duplicate"

	ErrTranslationWriteFailed = "navigation.err.translation.writeFailed"

	ErrTranslationWriteUnavailable = "navigation.err.translation.unavailable"
)

const (
	translationWriteRejectStatus = http.StatusConflict

	translationWriteFailureStatus = http.StatusInternalServerError
)

func ErrTranslationDuplicateError() error {
	return facing.WithStatus(facing.New(ErrTranslationDuplicate), translationWriteRejectStatus)
}

func ErrNotTranslatableRejected() error {
	return facing.WithStatus(facing.New(ErrNotTranslatable), translationWriteRejectStatus)
}

func ErrTranslationWriteFailedError() error {
	return facing.WithStatus(facing.New(ErrTranslationWriteFailed), translationWriteFailureStatus)
}

func ErrTranslationWriteUnavailableError() error {
	return facing.WithStatus(facing.New(ErrTranslationWriteUnavailable), translationWriteFailureStatus)
}
