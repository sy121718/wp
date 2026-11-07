package masterdatadto

import (
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/pagination"
)

// ChangeRowResp 只交枚举值与词条 key；成品译文由模板按请求语言取。
type ChangeRowResp struct {
	Time                string
	EntityType          string
	EntityTypeKey       string
	EntityTypeFallback  string
	EntityID            string
	EntityLabel         string
	Action              string
	ActionKey           string
	ActionFallback      string
	Field               string
	FieldKey            string
	FieldFallback       string
	OldValue            string
	NewValue            string
	Origin              string
	OperatorID          string
}

type EntityRowResp struct {
	EntityType         string
	EntityTypeKey      string
	EntityTypeFallback string
	EntityID           string
	EntityLabel        string
	ChangeCount        int64
	LastAction         string
	LastActionKey      string
	LastActionFallback string
	LastField          string
	LastFieldKey       string
	LastFieldFallback  string
	LastOperatorID     string
	LastAt             string

	URL string
}

type EntityTimelineHeadResp struct {
	EntityType         string
	EntityTypeKey      string
	EntityTypeFallback string
	EntityID           string
	EntityLabel        string
	Total              int64
}

// LabelOptionResp 只交枚举值与词条 key。
type LabelOptionResp struct {
	Value      string
	LabelKey   string
	LabelFallb string
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
