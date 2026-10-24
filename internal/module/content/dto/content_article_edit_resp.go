package contentdto

// 文章编辑页（/admin/articles/edit、/admin/articles/new）的强类型视图。
//
// 键名与模板 admin/content/article_edit.html 的点号取值一一对应（模板层契约，不是展示字段）。
// 分成 ArticleEditFormResp / ArticlePublishBlockResp / ArticleImportBlockResp 三个区块，
// 理由是**键集必须一致**这件事由类型系统保证，而不是由注释保证：
//
// 三个区块各有两个形态（可用 / 不可用），模板对它们用点号直取
// （{{.URLPath}} / {{.ImportProjects}} …）。Jet 遇到缺失的键不是渲染成空，
// 而是报错并**截断整页输出** —— 编辑页只剩上半截、状态码仍是 200，
// 看起来像「样式坏了」，极难联想到是少了一个键。
// 从前「键集齐全」靠三对函数互相抄（articlePublishView / articlePublishUnavailable、
// articleImportBlockView / articleImportUnavailable），漏一个键只能靠人眼比对；
// 现在不可用形态就是该结构体的零值（+ 提示事实），缺字段是编译错误。

// ArticleEditFormResp 编辑表单的回填值（新建时全部零值）。
//
// 与 POST 的字段名同源：表单控件 name 是 title/body/excerpt/featuredImage/focusKeyword，
// 这里是大写键供模板点号取值。
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

// ArticlePublishBlockResp 发布区块。
//
// 提示槽（PublishHint / NoTemplateHint）是**事实**（key + 中文兜底），不是拼好的句子：
// 取词发生在模板里。提前翻等于替渲染期做决定 —— 英文后台下会显示中文，且不报错、没日志。
type ArticlePublishBlockResp struct {
	PublishConfigured bool
	PublishHint       any
	Published         bool
	URLPath           string
	PublicURL         string
	Stale             bool
	Status            string
	DefaultURLPath    string
	// 两个下拉直接用本包既有的强类型选项（content_option_resp.go）：
	// 它们本来就是「ID + 已取好词的 Name」，没有理由为页面视图再造一份。
	Projects  []OptionResp
	Templates []TemplateOptionResp
	HasTemplates      bool
	NoTemplateHint    any
}

// ArticleImportBlockResp 「导入到画布」区块。
//
// 三个按钮的可用性条件在装配处判清楚：一个点了会 500 的按钮比不给按钮更糟。
// 新建中（还没有文章 id）、没有工程、页面能力未装配 —— 三种情况各给各的说法。
type ArticleImportBlockResp struct {
	ImportAvailable     bool
	ImportHint          any
	ImportProjects      []OptionResp
	ImportDefaultPath   string
	ImportHasPublished  bool
	ImportPreviewTarget string
}

// ArticleEditPageResp 文章编辑 / 新建整页。
type ArticleEditPageResp struct {
	// Title 是 i18n **key**（adminui.Prepare → injectI18n 按当前语言翻译它）。
	Title string `view:"title"`
	Menu  string

	IsNew bool
	// Form / Err 的 Err 是**事实**（key + 中文兜底），取词在模板里。
	Form ArticleEditFormResp
	Err  any

	InvalidField    string
	EditUnavailable bool
	ListURL         string

	// TemplateEditURL 必须在**任何装配状态下**都存在：模板里是 {{if .TemplateEditURL}}。
	TemplateEditURL string

	// Score 初始评分：已保存的正文直接算一遍，编辑者打开页面就能看到当前水平。
	Score any

	// 两个可选区块**匿名嵌入**（adminui.ViewMap 摊平匿名字段）：
	// 模板对它们用点号直取（{{.URLPath}} / {{.ImportProjects}}），不经过二级键。
	// 具名嵌套会让模板改成 {{.Publish.URLPath}} —— 那是模板改动，不是本轮的范围；
	// 而摊平之后「键集齐全」这件事仍然由这两个结构体保证（不可用形态 = 零值）。
	ArticlePublishBlockResp
	ArticleImportBlockResp
}
