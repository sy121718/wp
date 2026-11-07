package commentdto

import "go_wp/pkg/utils"

type Item struct {
	ID int64

	Body string

	IsReply bool

	CreatedAt utils.JSONTime

	Replies []Item
}

type ListResp struct {
	Items []Item

	Total int64
	Page  int

	PageSize int

	HasMore bool
}

type SubmitResp struct {
	ID int64

	Status string

	Message string
}

type AdminItem struct {
	ID         int64
	Body       string
	EntityType string
	EntityID   string

	UserID uint64
	Status string

	StatusLabel string
	CreateTime  utils.JSONTime

	ReviewedAt *utils.JSONTime

	ReviewerID *uint64

	IsReply bool
}

type AdminListResp struct {
	Items    []AdminItem
	Total    int64
	Page     int
	PageSize int
}

type ReviewResp struct {
	Affected int64

	Status string
}

type EntityType struct {
	Type string

	// Key 是文案词条 key，Fallback 是缺词兜底。
	// 后端只交事实，成品译文由调用方（模板）按请求语言取。
	Key string

	Label string
}
