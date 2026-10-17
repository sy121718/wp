package sysconfigdto

import "go_wp/pkg/utils"

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

// Group 一个配置分组（后台一次保存的单元）。
type Group struct {
	// Key 分组键（唯一）。
	Key string `json:"key"`
	// Name 分组显示名（后台页面展示）。
	Name string `json:"name"`
	// Data 整组配置的 JSON 对象。
	Data map[string]any `json:"data"`
	// Remark 备注。
	Remark string `json:"remark"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `json:"status"`
	// Version 乐观锁版本号：保存时必须带上读到的值，库内已变则拒绝。
	Version int64 `json:"version"`
	// UpdateTime 最后修改时间（对外只到秒）。
	UpdateTime utils.JSONTime `json:"updateTime"`
}
