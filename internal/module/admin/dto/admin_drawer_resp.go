package admindto

import "go_wp/pkg/pagination"

type DrawerSavedResp struct {
	Saved bool
}

type AdminListPageResp struct {
	Title       string `view:"title"`
	Menu        string `view:"menu"`
	Rows        any
	Total       int64
	FilterName  string
	FilterEmail string
	Err         string

	ListQuery string

	Pagination pagination.Page
}

type RuleListPageResp struct {
	Title        string `view:"title"`
	Menu         string `view:"menu"`
	Rows         any
	Total        int64
	Domains      any
	FilterDomain string
	Err          string
	ListQuery    string

	Pagination pagination.Page
}

type DepartmentListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Parents       any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string

	Pagination pagination.Page
}

type MenuListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Parents       any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string

	SearchMode bool

	PermChoices any

	Pagination pagination.Page
}

type RoleListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string

	Pagination pagination.Page
}

type DataruleEditPageResp struct {
	Title   string `view:"title"`
	Menu    string `view:"menu"`
	Detail  any
	Domains any

	Editor any
}

type PermissionEditFormResp struct {
	PermissionEdit any
}

type I18nEditFormResp struct {
	I18nEdit any
}

type RoleDrawerResp struct {
	Account any
	Rows    any

	Err string
}

type MenuDrawerResp struct {
	Account any
	Rows    any

	Err string
}

type MenuEditFormResp struct {
	MenuEdit    any
	PermChoices any

	Parents any

	MenuEditErr string

	MenuEditEcho map[string]string

	MenuEditEchoCodes []string

	MenuEditParent uint64
}

type PermissionEditFailResp struct {
	PermissionEdit     any
	PermissionEditErr  string
	PermissionEditEcho any
}

type RolePermissionsResp struct {
	Role any
	Rows any
}

type DataruleEditorResp struct {
	Editor any
}

type LoginPageResp struct {
	Title        string `view:"title"`
	CaptchaID    string `view:"captcha_id"`
	CaptchaImage string `view:"captcha_image"`

	DevLogin bool `view:"DevLogin"`
}

type I18nListPageResp struct {
	Title string `view:"title"`

	Entries      any
	I18nEditURLs []string

	Keyword    string
	LangFilter string
	CatFilter  string
	Categories []string

	LangOptions any

	Pagination pagination.Page
}

type PermListPageResp struct {
	Title string `view:"title"`
	Menu  string

	Rows  any
	Total int64

	FilterCode   string
	FilterModule string

	Err string

	ListQuery string

	Pagination pagination.Page
}
