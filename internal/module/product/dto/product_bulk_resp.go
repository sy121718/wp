package productdto

import "go_wp/pkg/facing"

type BulkResultResp struct {
	Done int `json:"done"`
}

type BulkRejectedError struct {
	Err   error
	Items []facing.BlockedItem
}

func (e *BulkRejectedError) Error() string { return e.Err.Error() }

func (e *BulkRejectedError) Unwrap() error { return e.Err }

func (e *BulkRejectedError) BlockedItems() []facing.BlockedItem { return e.Items }

type BulkPricingResultResp struct {
	BulkResultResp

	ChangedVariants int `json:"changedVariants"`

	UnchangedProducts int `json:"unchangedProducts"`
}
