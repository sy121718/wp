package userdto

import "go_wp/pkg/utils"

type SiteSessionResp struct {
	Authenticated bool `json:"authenticated"`
}

type SiteDevice struct {
	ID           string          `json:"id"`
	UserAgent    string          `json:"userAgent"`
	IP           string          `json:"ip"`
	Location     string          `json:"location"`
	LastActiveAt *utils.JSONTime `json:"lastActiveAt"`
	CreatedAt    *utils.JSONTime `json:"createdAt"`
	Current      bool            `json:"current"`
}
