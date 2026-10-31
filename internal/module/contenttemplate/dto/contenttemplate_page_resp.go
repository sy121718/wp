package contenttemplatedto

import (
	enums "go_wp/internal/module/contenttemplate/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/utils"
)

type TemplatePageRefResp struct {
	Label string
	Path  string
	Slots string
	URL   string
}

type TemplateInstanceRefResp struct {
	Label  string
	Entity string
	Path   string
	Slots  string
}

type TemplateListRowResp struct {
	ID           string
	Name         string
	EntityType   string
	DraftVersion int64

	UpdatedAt utils.JSONTime

	IsStructure bool

	Type      enums.LabelPair
	Role      enums.LabelPair
	IsDefault bool

	EditURL   string
	SampleErr string

	RefPages         []TemplatePageRefResp
	RefInstances     []TemplateInstanceRefResp
	RefCount         int
	RefPageCount     int
	RefInstanceCount int
}

type ContentTemplatesPageResp struct {
	Title string
	Menu  string

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	EntityType string

	Err string

	Ready bool

	Templates     []TemplateListRowResp
	TemplateCount int

	ImpactAvailable bool
	ImpactNote      string
}
