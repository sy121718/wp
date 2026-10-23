package contentdto

// content_option_resp.go — 内容模块的下拉选项形状。
//
// 三个下拉（工程 / 内容模板 / 页面）形状相同：ID + 已取好词的 Name。
// 合成一个类型比三份 gin.H 更可靠 —— 手写 map 里每处都可能拼错一个键，
// 而拼错的表现是选项渲染成空白（不报错）。
type OptionResp struct {
	ID   string
	Name string
}

// TemplateOptionResp 内容模板下拉（带草稿版本号：发布时要落定版本）。
type TemplateOptionResp struct {
	ID      string
	Name    string
	Version int64
}

// StalePageRowResp 待重建页面的一行（模板读 .ID / .Path / .ProjectName）。
type StalePageRowResp struct {
	ID          string
	Path        string
	ProjectID   string
	ProjectName string
}

// StaleImpactResp 「待重建页面影响面」的渲染数据。
//
// 降级语义：失败 → Available=false（显示「读不到」），**绝不**渲染成「0 个待重建」
// （那会把一次读取失败伪装成一切正常）。
//
// Hint 是**词条事实**（key + 中文兜底）不是拼好的句子：取词在模板渲染期发生。
// 控制器不取词 —— 语言是请求级的，在那里取词意味着「英文后台显示中文」这类
// 不会报错、也不会被测出来的缺陷。
type StaleImpactResp struct {
	Available bool
	Pages     []StalePageRowResp
	Total     int
	Truncated bool
	Limit     int
	Hint      any
}
