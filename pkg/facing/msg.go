// msg.go — 一句要展示的文案的**事实**（key + 中文兜底 + `%s` 位置参数）。
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
// 三段各自的判据：
//   - Key 为空 → Fallback 就是成品串（给「一段 key 表达不了」的复合句：多段词条拼接、
//     或带明细的错误）；
//   - Fallback 不给空串 —— 空串会让失败在页面上**完全不可见**，用户以为操作成功了；
//   - Args 一律是**已 strconv 的字符串**：模板侧的占位符是 `%s`，传 int 会渲染成
//     `%!s(int=3)`。
package facing

// Msg 一句可展示文案的事实。
type Msg struct {
	Key      string
	Fallback string
	Args     []string
}

// WithArgs 带上 `%s` 位置参数（数字先 strconv.Itoa）。值语义返回，可链式：
//
//	facing.Msg{Key: k, Fallback: "已删除 %s 个页面。"}.WithArgs(strconv.Itoa(n))
func (m Msg) WithArgs(args ...string) Msg {
	m.Args = args
	return m
}

// Empty 是否是「什么都没有」的事实（拿不到 err 的失败分支用它判断要不要给默认值）。
func (m Msg) Empty() bool {
	return m.Key == "" && m.Fallback == ""
}
