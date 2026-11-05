package admindto

import (
	"go_wp/pkg/utils"
)

type PermItem struct {
	ID             uint64          `json:"id"`
	PermissionCode string          `json:"permission_code"`
	PermissionName string          `json:"permission_name"`
	Module         string          `json:"module"`
	APIPath        string          `json:"api_path"`
	APIMethod      string          `json:"api_method"`
	Status         int             `json:"status"`
	Remark         string          `json:"remark"`
	CreateTime     *utils.JSONTime `json:"create_time"`
}

type PermListResp struct {
	Total int64      `json:"total"`
	List  []PermItem `json:"list"`
}

type PermDetailResp struct {
	ID             uint64          `json:"id"`
	PermissionCode string          `json:"permission_code"`
	PermissionName string          `json:"permission_name"`
	Module         string          `json:"module"`
	APIPath        string          `json:"api_path"`
	APIMethod      string          `json:"api_method"`
	Status         int             `json:"status"`
	Remark         string          `json:"remark"`
	CreateTime     *utils.JSONTime `json:"create_time"`
	UpdateTime     *utils.JSONTime `json:"update_time"`
}

type PermOptionItem struct {
	ID             uint64 `json:"id"`
	PermissionCode string `json:"permission_code"`
	PermissionName string `json:"permission_name"`
	Module         string `json:"module"`
	APIPath        string `json:"api_path"`
	APIMethod      string `json:"api_method"`
}

type PermOptionsResp struct {
	List []PermOptionItem `json:"list"`
}

type PermCreateResp struct {
	ID uint64 `json:"id"`
}

type PermUpdateResp struct {
	ID uint64 `json:"id"`
}

type PermDeleteResp struct {
	DeletedCount int64 `json:"deleted_count"`
}

type PermBrief struct {
	PermissionCode string `json:"permission_code"`
	PermissionName string `json:"permission_name"`
	APIPath        string `json:"api_path"`
	APIMethod      string `json:"api_method"`
	Status         int    `json:"status"`
}

type PermListByCodesResp struct {
	List []PermBrief `json:"list"`
}
