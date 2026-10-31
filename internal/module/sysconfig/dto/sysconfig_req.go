package sysconfigdto

type SetGroupsReq struct {
	Groups []SetGroupReq

	UpdateBy int64
}

type SetGroupReq struct {
	GroupKey string `json:"groupKey" form:"groupKey"`

	Data map[string]any `json:"data" form:"data"`

	Version int64 `json:"version" form:"version"`

	UpdateBy int64 `json:"updateBy" form:"updateBy"`
}
