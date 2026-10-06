package admindto

// MenuTreeReq 菜单树查询。
type MenuTreeReq struct {
	Status *int   `form:"status" json:"status"`
	Type   *int   `form:"type" json:"type"`
	Search string `form:"search" json:"search" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

// MenuPageRow 是菜单管理列表的一行：**树状平铺**（顶级行连同它的子孙一起按 DFS 前序排开，
// 层级由 Depth 表达）。分页单位是顶级节点，所以一页里的每一段子树都是完整的。
//
// 这里没有 ParentTitle：树状列表里父级就是上一行，缩进已经把「谁挂在谁下面」说清楚了，
// 每行再重复一个父级名只会让列变宽、且同一父级下的行重复显示同一个值。
type MenuPageRow struct {
	ID        uint64
	ParentID  uint64
	Title     string
	Path      string
	Type      int
	Status    int
	SortOrder int
	Remark    string
	Icon      string
	// PermissionCodes 是该菜单挂的全部权限码（迁移 470 起多对多）。
	// 列表页据此显示「这个节点代表哪些权限」；空切片表示没绑（目录 / iframe / 外链是常态）。
	PermissionCodes []string
	// Depth 是相对本页顶级节点的层级（顶级为 0），服务端算好，模板据此缩进 ——
	// 模板不自己推层级，也不需要第二套父子规则。
	Depth int
	// HasChildren 表示**本页里有**以它为前提的子行；折叠三角只在为真时渲染。
	HasChildren bool
	// Hidden 是初始不显示（某个祖先处于折叠态）：服务端给初始态，前端只负责切换。
	// 用「隐藏」而不是「展开」表达，是因为 Vue/HTMX 都不用管状态同步 ——
	// 行本身是不是可见，一眼就能从 HTML 里读出来。
	Hidden bool
	// Expanded 是搜索态下「本行初始展开」（命中路径上的祖先，好让命中行露出来）。
	// 浏览态恒为假：列表默认只显示最上级。
	Expanded bool
	// Matched 是搜索命中项（浏览态恒为假）。其余行是「仅供定位」的上级路径，模板据此区分。
	Matched bool
}

// MenuParentChoice is an unpaged selector option.
type MenuParentChoice struct {
	ID     uint64
	Title  string
	Indent string
	Type   int
	// Disabled 标出「不能作为它的上级」的候选项（编辑态：自身与自身的子孙）。
	//
	// 仍然渲染（而不是从候选里剔掉）：用户要能看见自己的子孙在层级里的位置，
	// 才明白为什么点不了；直接消失会让人以为候选丢了、转而怀疑下拉坏了。
	// 新建态恒为 false —— 新建的菜单还没有子孙。
	Disabled bool
}

// MenuPageResp 是菜单管理列表的分页读结果。
//
// Rows 是**树状摊平的行**：Total 是分页单位的数量 —— 浏览态 = 顶级菜单数，
// 搜索态 = 命中所属的顶级菜单数（搜索按根分页，否则同一棵树会跨页重复）。
// 它不等于行数：一个顶级节点摊开是一整棵子树。
type MenuPageResp struct {
	Total int64
	Rows  []MenuPageRow
	// Parents 是新建 / 编辑抽屉的上级菜单下拉候选（**不分页、全量**）：
	// 缺项会直接表现为「建子菜单时选不到父级」。
	Parents []MenuParentChoice
}

// MenuDetailReq 查询单个菜单详情。
type MenuDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// MenuCreateReq 新建菜单。
type MenuCreateReq struct {
	// PermissionCodes 是提交的全部权限码（迁移 470 起多对多，空值由 service 侧丢弃）。
	// 菜单（type=2）与按钮（type=3）至少一个，目录 / iframe / 外链必须为空 —— 判据在
	// validatePermissionBinding，这里只做长度上限（单个码 100，最多 50 个）。
	PermissionCodes []string `json:"permission_codes" binding:"max=50,dive,max=100" validate:"max=50,dive,max=100"`
	Title           string   `json:"title" binding:"required,max=50" validate:"required,max=50"`
	TitleKey        *string  `json:"title_key" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ParentID        uint64   `json:"parent_id"`
	Type            int      `json:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path            string   `json:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ExternalURL     string   `json:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon            string   `json:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status          int      `json:"status"`
	IsHidden        int      `json:"is_hidden"`
	IsPublic        int      `json:"is_public"`
	SortOrder       int      `json:"sort_order"`
	Remark          string   `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// MenuUpdateReq 更新菜单。
type MenuUpdateReq struct {
	ID uint64 `json:"id" binding:"required" validate:"required"`
	// 全量替换语义：提交的集合就是该菜单的完整权限集合，未列出的会被撤销。
	PermissionCodes []string `json:"permission_codes" binding:"max=50,dive,max=100" validate:"max=50,dive,max=100"`
	Title           string   `json:"title" binding:"required,max=50" validate:"required,max=50"`
	TitleKey        *string  `json:"title_key" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ParentID        uint64   `json:"parent_id"`
	Type            int      `json:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path            string   `json:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ExternalURL     string   `json:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon            string   `json:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status          int      `json:"status"`
	IsHidden        int      `json:"is_hidden"`
	IsPublic        int      `json:"is_public"`
	SortOrder       int      `json:"sort_order"`
	Remark          string   `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// MenuDeleteReq 批量删除菜单。
type MenuDeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1" validate:"required,min=1"`
}

// MenuTreeNode 菜单树节点（前端渲染用）。
type MenuTreeNode struct {
	ID              uint64         `json:"id"`
	PermissionCodes []string       `json:"permission_codes"`
	Title           string         `json:"title"`
	TitleKey        string         `json:"title_key,omitempty"`
	ParentID        uint64         `json:"parent_id"`
	Type            int            `json:"type"`
	Path            string         `json:"path"`
	ExternalURL     string         `json:"external_url"`
	Icon            string         `json:"icon"`
	Status          int            `json:"status"`
	IsHidden        int            `json:"is_hidden"`
	IsPublic        int            `json:"is_public"`
	IsSystem        int            `json:"is_system"`
	SortOrder       int            `json:"sort_order"`
	Remark          string         `json:"remark"`
	Children        []MenuTreeNode `json:"children,omitempty"`
}

// MenuDetailResp 菜单详情。
type MenuDetailResp struct {
	ID              uint64   `json:"id"`
	PermissionCodes []string `json:"permission_codes"`
	Title           string   `json:"title"`
	TitleKey        *string  `json:"title_key,omitempty"`
	ParentID        uint64   `json:"parent_id"`
	Type            int      `json:"type"`
	Path            string   `json:"path"`
	ExternalURL     string   `json:"external_url"`
	Icon            string   `json:"icon"`
	Status          int      `json:"status"`
	IsHidden        int      `json:"is_hidden"`
	IsPublic        int      `json:"is_public"`
	IsSystem        int      `json:"is_system"`
	SortOrder       int      `json:"sort_order"`
	Remark          string   `json:"remark"`
}
