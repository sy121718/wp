package sysconfigdto

// sysconfig_dict.go — 字典下拉的展示形状（数据流 model → service → inbound）。

// DictOption 一个字典下拉项。
type DictOption struct {
	// Code 值（语言形如 zh-CN，货币形如 CNY）。
	Code string `json:"code"`
	// Label 展示文案（语言用 code 本身，货币用「CNY ¥」）。
	Label string `json:"label"`
	// Symbol 货币符号（仅 currency 类型有值；语言为空）。
	//
	// 与 Label 分开给出，是因为两者的用途不同：下拉里要的是「CNY ¥」这种
	// 代码加符号的完整标签，而**金额展示**只要那个符号（¥300.50）。
	// 让调用方从 Label 里切符号是做得到但会静默坏掉的做法 ——
	// 标签格式一改（加空格、换顺序）切出来的东西就不对了，且不报错。
	Symbol string `json:"symbol,omitempty"`
	// UIAvailable 该语言的界面文案是否已有译文（sys_dict.ui_available）。
	//
	// 消费方按用途决定要不要展示它：i18n 词条页的「新建词条」语言下拉用它标记
	// （「我该给哪个语言补词条」），而筛选下拉不展示（筛的是已有词条，标记是噪音）。
	UIAvailable bool `json:"uiAvailable"`
}

// CountryOption 一个国家下拉项。
//
// Label 按当前界面语言取中文或英文名：sys_area 两列都有（name_zh / name_en），
// 由 service 依当前语言挑一列 —— 下拉里显示「安哥拉 / Angola」取决于是谁在看。
type CountryOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// SetGroupsReq 多组一次保存（同一事务）。
//
// 为什么不是「循环调 SetGroup」：系统设置页一张表单同时改 i18n 与 trade 两组，
// 逐组独立提交会出现「语言改了、货币没改」的半截状态，而界面上它们是一次提交。
type SetGroupsReq struct {
	// Groups 要保存的组（每组自带 version 乐观锁前置条件与完整数据）。
	Groups []SetGroupReq
	// UpdateBy 操作人 ID（0 = 系统 / 未登录）。
	UpdateBy int64
}
