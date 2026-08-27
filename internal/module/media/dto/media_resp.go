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
}

// ListResp 附件列表响应。
type ListResp struct {
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Limit int              `json:"limit"`
	List  []AttachmentResp `json:"list"`
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
