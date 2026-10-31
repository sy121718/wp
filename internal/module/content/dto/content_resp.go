package contentdto

type ContentResp struct {
	ID         string         `json:"id"`
	EntityType string         `json:"entityType"`
	Slug       string         `json:"slug"`
	Revision   int64          `json:"revision"`
	Data       map[string]any `json:"data"`
	UpdatedAt  string         `json:"updatedAt"`
}
