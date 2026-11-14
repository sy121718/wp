package contenttemplatedto

import (
	enums "go_wp/internal/module/contenttemplate/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/facing"
	"go_wp/pkg/utils"
)

type TemplatePageRefResp struct {
	Label string
	Path  string
	// Slots 是槽位事实（key + 兜底），由模板取词。
	Slots []enums.LabelPair
	URL   string
}

type TemplateInstanceRefResp struct {
	Label  string
	Entity string
	Path   string
	// Slots 是槽位事实（key + 兜底），由模板取词。
	Slots []enums.LabelPair
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

	EditURL string

	// SampleNote 是「没有可视化编辑入口」的原因事实（key + 兜底），由模板取词。
	SampleNote facing.Msg

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

	// Err 是列表取数失败的错误事实（key + 兜底），由模板取词。
	Err facing.Msg

	Ready bool

	Templates     []TemplateListRowResp
	TemplateCount int

	ImpactAvailable bool

	// ImpactNote 是引用面「查不出 / 可能不完整」的提示事实，由模板取词。
	ImpactNote facing.Msg
}
