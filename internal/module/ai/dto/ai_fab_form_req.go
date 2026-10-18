package aidto

// FabAskFormReq 悬浮球提问的表单入参（transport 形状）。
//
// 图片由前端读成 data URI 一起传上来（服务端不去取任何地址，见 ai_fab_image.go），
// 因此 Images / ImageLabels 是两列逗号分隔（或换行分隔）的字符串，解析留在 handler。
type FabAskFormReq struct {
	Input       string `form:"input"`
	Images      string `form:"images"`
	ImageLabels string `form:"imageLabels"`
}
