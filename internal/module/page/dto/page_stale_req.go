package pagedto

type StalePageListReq struct {
	ProjectID string

	Limit int

	OrderBy string

	Descending bool
}
