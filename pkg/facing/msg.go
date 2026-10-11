// msg.go — 一句要展示的文案的**事实**（key + 中文兜底 + 参数）。
//
// 为什么要有这个类型：控制器/服务不再「取词」，它只把「要说什么」交出去 —— 一句文案由
// 三部分组成：i18n key（决定语言）、中文兜底（词条缺失时不显示裸 key）、位置参数
// （计数这类运行时数字）。三者合成一个值传递，由**渲染期**用取词函数填充
// （后台页面是 Jet 模板里的 tf，JSON 出口是出口包自己取）。
//
// 为什么归本包而不是各模块自己定、也不是各出口自己定：八个模块各造一个同形 struct
// 就是八份契约（往渲染数据里塞的字段名、参数编码方式迟早分叉，症状是「某个页面的提示
// 偶尔少一个数字」，不报错也不记日志）；而把它放在某一条出口（后台 UI / JSON 响应）里，
// 另外两条出口就得各留一份自己的说法。这里是**唯一**一份。
//
// 四段各自的判据：
//   - Key 为空 → Fallback 就是成品串（给「一段 key 表达不了」的复合句：多段词条拼接、
//     或带明细的错误）；
//   - Fallback 不给空串 —— 空串会让失败在页面上**完全不可见**，用户以为操作成功了；
//   - Args / Named 一律是**已 strconv 的字符串**：占位符是 `%s` / `{name}`，传 int 会
//     渲染成 `%!s(int=3)`，或让 `{name}` 填不上（填不干净会整条回落兜底）；
//   - Args 与 Named 不同时给 —— 判据是「语序归谁」，两种参数混在一句里没有第二种解释。
package facing

// Msg 一句可展示文案的事实。
//
// 参数有两种形态，**并存**而不是二选一：
//   - Args：`%s` 位置参数，语序由**调用方**按词条顺序决定（存量词条多用这一种）；
//   - Named：`{name}` 具名参数，语序由**词条**决定 —— 运营改词条的语序不会改坏调用方代码。
//
// 两者都给时用具名（分派在 adminui.MsgText）。为什么保留两种：位置参数那批词条是
// **已 seed 的数据**（运营可能改过），改写它们属于独立的文案迁移，不是「加一种参数
// 形态」能顺带做的 —— 理由与 internal/adminui/page_error.go 的 TranslateArgs 一致。
type Msg struct {
	Key      string
	Fallback string
	Args     []string
	Named    map[string]string
}

// WithArgs 带上 `%s` 位置参数（数字先 strconv.Itoa）。值语义返回，可链式：
//
//	facing.Msg{Key: k, Fallback: "已删除 %s 个页面。"}.WithArgs(strconv.Itoa(n))
func (m Msg) WithArgs(args ...string) Msg {
	m.Args = args
	return m
}

// WithNamed 带上 `{name}` 具名参数（值同样先 strconv 成字符串）。值语义返回，可链式：
//
//	facing.Msg{Key: k, Fallback: "已删除 {n} 个页面。"}.WithNamed(map[string]string{"n": "3"})
func (m Msg) WithNamed(params map[string]string) Msg {
	m.Named = params
	return m
}

// Empty 是否是「什么都没有」的事实（拿不到 err 的失败分支用它判断要不要给默认值）。
func (m Msg) Empty() bool {
	return m.Key == "" && m.Fallback == ""
}
