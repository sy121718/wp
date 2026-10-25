package membershipdto

// membership_option_resp.go — 会员模块的下拉选项形状。
//
// 两种形状分开声明，不合并：
//   · ValueOption —— 只有 Value + Selected（来源下拉：值本身就是内建常量，没有可读名）
//   · TierOption  —— Value + Label + Selected（等级下拉：Label 是**运营输入的数据**，
//     不是 UI 文案，所以它进视图而不是由模板取词）
//
// 合成一个会让「这个下拉的 Label 从哪来」变得不可读 —— 而这两者的差别正是
// 「服务端拼 UI 文案」（禁止）与「服务端给运营数据」（必需）的分界。

// ValueOption 只有取值与选中态的下拉项（来源下拉）。
type ValueOption struct {
	Value    string
	Selected bool
}

// TierOption 等级下拉项（Label 是等级名，运营输入的数据）。
type TierOption struct {
	Value    int64
	Label    string
	Selected bool
}
