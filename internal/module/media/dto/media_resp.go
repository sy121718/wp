package mediadto

// AttachmentResp 附件响应。
type AttachmentResp struct {
	ID          uint64  `json:"id"`
	CategoryID  *uint64 `json:"category_id"`
	FileName    string  `json:"file_name"`
	FileSize    int64   `json:"file_size"`
	FileType    string  `json:"file_type"`
	MimeType    string  `json:"mime_type"`
	StorageType string  `json:"storage_type"`
	URL         string  `json:"url"`
	MD5         string  `json:"md5"`
	ExtraInfo   string  `json:"extra_info"`
	CreateTime  string  `json:"create_time"`
	// Generation 换图代数（迁移 067）：初始 1，每次换图 +1。
	Generation int `json:"generation"`
	// Duplicate 本次上传命中 md5+类型去重、复用已有附件（URL/变体为既有记录的）。
	Duplicate bool `json:"duplicate,omitempty"`
	// Variants 图片变体列表（仅图片类附件有值；非图片为空）。
	Variants []VariantResp `json:"variants,omitempty"`
}

// VariantRef 已就绪的图片变体引用（构建期 srcset 消费）。
//
// 为什么返回 URL 而不是只返回宽度：变体文件名带内容指纹
// （<stem>_<type>-<generation>-<hash8>.jpg），而 generation 与 hash 只有 media 模块
// 知道 —— 让调用方按宽度自己拼文件名，等于把命名约定抄成第二份真源，
// 命名一改就是「srcset 静默指向不存在的文件」这种最难查的类型。
type VariantRef struct {
	URL   string `json:"url"`
	Width int    `json:"width"`
}

// VariantResp 图片变体响应（thumb/medium/full 生成状态与结果）。
type VariantResp struct {
	VariantType string `json:"variant_type"`
	Status      string `json:"status"`
	FilePath    string `json:"file_path,omitempty"`
	URL         string `json:"url,omitempty"`
	FileSize    int64  `json:"file_size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	MimeType    string `json:"mime_type,omitempty"`
}

// DownloadEntry 打包下载条目：Content 非空时直接写入字节（README 说明），否则读取本地 Path。
type DownloadEntry struct {
	Name    string `json:"name"`              // zip 内路径（已做安全化）
	Path    string `json:"path,omitempty"`    // 本地文件绝对路径
	Content string `json:"content,omitempty"` // 内联内容（README.txt）
}

// DownloadPlan 一次打包下载的完整计划，handler 据此流式写 zip（不落盘临时文件）。
type DownloadPlan struct {
	FileName string          `json:"file_name"`
	Entries  []DownloadEntry `json:"entries"`
}

// ListResp 附件列表响应。
type ListResp struct {
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Limit int              `json:"limit"`
	List  []AttachmentResp `json:"list"`
	// NextCursor 为空表示已经到达结果集末尾。
	NextCursor string `json:"next_cursor,omitempty"`
}

// CategoryTreeNode 分类树节点。
type CategoryTreeNode struct {
	ID           uint64             `json:"id"`
	CategoryName string             `json:"category_name"`
	CategoryCode string             `json:"category_code"`
	ParentID     uint64             `json:"parent_id"`
	SortOrder    int                `json:"sort_order"`
	Children     []CategoryTreeNode `json:"children,omitempty"`
}
