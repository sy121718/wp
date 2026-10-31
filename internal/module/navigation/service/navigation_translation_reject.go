package navigationservice

import (
	"go_wp/pkg/facing"
)

type TranslationRejectedError struct {
	Err error

	Items []facing.BlockedItem
}

func (e *TranslationRejectedError) Error() string { return e.Err.Error() }

func (e *TranslationRejectedError) Unwrap() error { return e.Err }

func (e *TranslationRejectedError) BlockedItems() []facing.BlockedItem { return e.Items }

var _ facing.BlockedError = (*TranslationRejectedError)(nil)
