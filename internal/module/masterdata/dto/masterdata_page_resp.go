package masterdatadto

import (
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/pagination"
)

type ChangeRowResp struct {
	Time            string
	EntityType      string
	EntityTypeLabel string
	EntityID        string
	EntityLabel     string
	Action          string
	ActionLabel     string
	Field           string
	FieldLabel      string
	OldValue        string
	NewValue        string
	Origin          string
	OperatorID      string
}

type EntityRowResp struct {
	EntityType      string
	EntityTypeLabel string
	EntityID        string
	EntityLabel     string
	ChangeCount     int64
	LastAction      string
	LastActionLabel string
	LastField       string
	LastFieldLabel  string
	LastOperatorID  string
	LastAt          string

	URL string
}

type EntityTimelineHeadResp struct {
	EntityType      string
	EntityTypeLabel string
	EntityID        string
	EntityLabel     string
	Total           int64
}

type LabelOptionResp struct {
	Value string
	Label string
}

type MasterDataChangesPageResp struct {
	Title string
	Menu  string

	Projects        []projectdto.ProjectResp
	SelectedProject string

	EntityTypes []LabelOptionResp
	Actions     []LabelOptionResp

	FilterEntityType string
	FilterEntityID   string
	FilterField      string
	FilterAction     string
	FilterKeyword    string
	FilterOperatorID string
	FilterSince      string
	FilterUntil      string
	FilterSpecific   bool

	ViewMode string

	RecordsViewURL  string
	EntitiesViewURL string

	Current *EntityTimelineHeadResp

	Rows  []ChangeRowResp
	Total int64

	Entities    []EntityRowResp
	EntityTotal int64

	EntityShown    int
	EntityPageSize int

	EntityTruncated bool

	Err string

	LoadFailed bool

	Pagination pagination.Page
}
