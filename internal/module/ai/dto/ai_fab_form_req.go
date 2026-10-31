package aidto

type FabAskFormReq struct {
	Input       string `form:"input"`
	Images      string `form:"images"`
	ImageLabels string `form:"imageLabels"`
}
