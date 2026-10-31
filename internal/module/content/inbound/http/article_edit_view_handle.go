package contenthttp

type ArticleEditFormResp struct {
	ID            string
	Slug          string
	Title         string
	Body          string
	Excerpt       string
	FeaturedImage string
	FocusKeyword  string
	Revision      int64
	UpdatedAt     string
}

type ArticlePublishBlockResp struct {
	PublishConfigured bool
	PublishHint       any
	Published         bool
	URLPath           string
	PublicURL         string
	Stale             bool
	Status            string
	DefaultURLPath    string

	Projects       []OptionResp
	Templates      []TemplateOptionResp
	HasTemplates   bool
	NoTemplateHint any
}

type ArticleImportBlockResp struct {
	ImportAvailable     bool
	ImportHint          any
	ImportProjects      []OptionResp
	ImportDefaultPath   string
	ImportHasPublished  bool
	ImportPreviewTarget string
}

type ArticleEditPageResp struct {
	Title string `view:"title"`
	Menu  string

	IsNew bool

	Form ArticleEditFormResp
	Err  any

	InvalidField    string
	EditUnavailable bool
	ListURL         string

	TemplateEditURL string

	Score any

	ArticlePublishBlockResp
	ArticleImportBlockResp
}
