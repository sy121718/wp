package projectcontract

import (
	"context"

	projectdto "go_wp/internal/module/project/dto"
)

type (
	CreateReq           = projectdto.CreateReq
	UpdateReq           = projectdto.UpdateReq
	DetailReq           = projectdto.DetailReq
	ProjectResp         = projectdto.ProjectResp
	RetentionPolicyResp = projectdto.RetentionPolicyResp
	ThemeCreateReq      = projectdto.ThemeCreateReq
	ThemeUpdateReq      = projectdto.ThemeUpdateReq
	ThemeActivateReq    = projectdto.ThemeActivateReq
	ThemeResp           = projectdto.ThemeResp
	LocaleItem          = projectdto.LocaleItem
	LocalesSaveReq      = projectdto.LocalesSaveReq
	LocaleResp          = projectdto.LocaleResp

	SiteSettings = projectdto.SiteSettings
)

type RetentionPolicyReader interface {
	ListRetentionPolicies(ctx context.Context) (res []projectdto.RetentionPolicyResp, err error)
}

var ParseSiteSettings = projectdto.ParseSiteSettings

type ProjectService interface {
	SetLocaleRetirePort(port LocaleRetirePort)
	Create(ctx context.Context, req *projectdto.CreateReq) (res *projectdto.ProjectResp, err error)

	List(ctx context.Context) (res []projectdto.ProjectResp, err error)
	Detail(ctx context.Context, req *projectdto.DetailReq) (res *projectdto.ProjectResp, err error)
	Update(ctx context.Context, req *projectdto.UpdateReq) (res *projectdto.ProjectResp, err error)
	Exists(ctx context.Context, id string) (exists bool, err error)

	ListThemes(ctx context.Context, projectID string) (res []projectdto.ThemeResp, err error)

	ListThemesByBlockID(ctx context.Context, blockID string) (res []projectdto.ThemeResp, err error)

	GetTheme(ctx context.Context, id string) (res *projectdto.ThemeResp, err error)

	CreateTheme(ctx context.Context, req *projectdto.ThemeCreateReq) (res *projectdto.ThemeResp, err error)

	UpdateTheme(ctx context.Context, req *projectdto.ThemeUpdateReq) (res *projectdto.ThemeResp, err error)

	ActivateTheme(ctx context.Context, req *projectdto.ThemeActivateReq) (err error)

	DeleteTheme(ctx context.Context, id string) (err error)

	GetActiveTheme(ctx context.Context, projectID string) (res *projectdto.ThemeResp, err error)

	EnsureDefaultThemes(ctx context.Context) (fixed int, err error)

	ListLocales(ctx context.Context, projectID string) (res []projectdto.LocaleResp, err error)

	EnabledLangs(ctx context.Context, projectID string) (langs []string, err error)

	DefaultLocale(ctx context.Context, projectID string) (lang string, err error)

	SiteLangURLMode(ctx context.Context, projectID string) (raw string, err error)

	SaveLocales(ctx context.Context, req *projectdto.LocalesSaveReq) (res []projectdto.LocaleResp, err error)

	StructureTemplateOptions(ctx context.Context, projectID string) (opts []StructureTemplateOption, err error)
}

type StructureTemplateOption struct {
	ID   string
	Name string

	EntityType string

	IsDefault bool
}

type StructureTemplateOptionsPort interface {
	ListStructureTemplateOptions(ctx context.Context, projectID string) (opts []StructureTemplateOption, err error)
}

type LocaleRetirePort interface {
	LocaleRetireImpact(ctx context.Context, projectID, lang string) (affected int, err error)

	RetireLocale(ctx context.Context, projectID, lang string) (retired int, err error)
}
