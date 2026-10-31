package mediadto

type AttachmentRefResp struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

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

	Generation int `json:"generation"`

	Duplicate bool `json:"duplicate,omitempty"`

	Variants []VariantResp `json:"variants,omitempty"`
}

type VariantRef struct {
	URL   string `json:"url"`
	Width int    `json:"width"`
}

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

type DownloadEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
}

type DownloadPlan struct {
	FileName string          `json:"file_name"`
	Entries  []DownloadEntry `json:"entries"`
}

type ListResp struct {
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Limit int              `json:"limit"`
	List  []AttachmentResp `json:"list"`

	NextCursor string `json:"next_cursor,omitempty"`
}

type CategoryTreeNode struct {
	ID           uint64             `json:"id"`
	CategoryName string             `json:"category_name"`
	CategoryCode string             `json:"category_code"`
	ParentID     uint64             `json:"parent_id"`
	SortOrder    int                `json:"sort_order"`
	Children     []CategoryTreeNode `json:"children,omitempty"`
}
