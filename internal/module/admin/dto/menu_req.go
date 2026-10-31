package admindto

type MenuTreeReq struct {
	Status *int   `form:"status" json:"status"`
	Type   *int   `form:"type" json:"type"`
	Search string `form:"search" json:"search" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

type MenuDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

type MenuCreateReq struct {
	PermissionCodes []string `json:"permission_codes" form:"permission_codes" binding:"max=50,dive,max=100" validate:"max=50,dive,max=100"`
	Title           string   `json:"title" form:"title" binding:"required,max=50" validate:"required,max=50"`
	TitleKey        *string  `json:"title_key" form:"title_key" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ParentID        uint64   `json:"parent_id" form:"parent_id"`
	Type            int      `json:"type" form:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path            string   `json:"path" form:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ExternalURL     string   `json:"external_url" form:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon            string   `json:"icon" form:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status          int      `json:"status" form:"status"`
	IsHidden        int      `json:"is_hidden" form:"is_hidden"`
	IsPublic        int      `json:"is_public" form:"is_public"`
	SortOrder       int      `json:"sort_order" form:"sort_order"`
	Remark          string   `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

type MenuUpdateReq struct {
	ID uint64 `json:"id" form:"id" binding:"required" validate:"required"`

	PermissionCodes []string `json:"permission_codes" form:"permission_codes" binding:"max=50,dive,max=100" validate:"max=50,dive,max=100"`
	Title           string   `json:"title" form:"title" binding:"required,max=50" validate:"required,max=50"`
	TitleKey        *string  `json:"title_key" form:"title_key" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ParentID        uint64   `json:"parent_id" form:"parent_id"`
	Type            int      `json:"type" form:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path            string   `json:"path" form:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ExternalURL     string   `json:"external_url" form:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon            string   `json:"icon" form:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status          int      `json:"status" form:"status"`
	IsHidden        int      `json:"is_hidden" form:"is_hidden"`
	IsPublic        int      `json:"is_public" form:"is_public"`
	SortOrder       int      `json:"sort_order" form:"sort_order"`
	Remark          string   `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

type MenuDeleteReq struct {
	IDs []uint64 `json:"ids" form:"ids" binding:"required,min=1" validate:"required,min=1"`
}
