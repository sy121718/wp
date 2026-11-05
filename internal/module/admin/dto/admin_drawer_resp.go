package admindto

import "go_wp/pkg/pagination"

type AdminListPageResp struct {
	Menu        string `view:"menu"`
	Rows        any
	Total       int64
	FilterName  string
	FilterEmail string

	ListQuery string

	Pagination pagination.Page
}

type RuleListPageResp struct {
	Menu         string `view:"menu"`
	Rows         any
	Total        int64
	Domains      any
	FilterDomain string
	ListQuery    string

	Pagination pagination.Page
}

type DepartmentListPageResp struct {
	Menu          string `view:"menu"`
	Rows          any
	Parents       any
	Total         int64
	FilterKeyword string
	ListQuery     string

	Pagination pagination.Page
}

type MenuListPageResp struct {
	Menu          string `view:"menu"`
	Rows          any
	Parents       any
	Total         int64
	FilterKeyword string
	ListQuery     string

	SearchMode bool

	PermChoices any

	Pagination pagination.Page
}

type RoleListPageResp struct {
	Menu          string `view:"menu"`
	Rows          any
	Total         int64
	FilterKeyword string
	ListQuery     string

	Pagination pagination.Page
}

type DataruleEditPageResp struct {
	Menu    string `view:"menu"`
	Detail  *RuleDetailResp
	Domains []RuleDomainItem
	Editor  *RuleDomainDetail
}

type LoginPageResp struct {
	Title        string `view:"title"`
	CaptchaID    string `view:"captcha_id"`
	CaptchaImage string `view:"captcha_image"`

	DevLogin bool `view:"DevLogin"`
}

type PermListPageResp struct {
	Menu string

	Rows  any
	Total int64

	FilterCode   string
	FilterModule string

	ListQuery string

	Pagination pagination.Page
}
