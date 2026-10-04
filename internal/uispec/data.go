package uispec

// 本文件定义「工具 → 渲染器」的数据形状。
//
// 为什么工具要按这些形状返回，而不是各自返回自己的 DTO：渲染器必须能**通用**地渲染
// 任意数据源的结果。如果每个工具的 Data 都是自己的 DTO，渲染器就只能为每个工具写一个
// 分支 —— 那就是把「组件白名单」偷换成「数据源白名单」，加一个工具要改渲染器。
//
// 边界：这些形状只描述**怎么显示**，不含业务判断（排序、口径、哪些行该出现）。
// 那是数据源（工具）的事 —— 渲染器拿到什么就画什么。这样「同一份数据在页面上和在 AI 回答里
// 不一致」这类问题被限制在一处：口径只有工具一份。
//
// 渲染器用类型断言选择画法（switch data.(type)），未知形状 = 渲染不了 → 该块降级。
// 这也是为什么不需要额外注册表：形状本身就是白名单。

// Column 表格的一列。
type Column struct {
	// Label 表头文案。
	Label string
	// Align 对齐："" 或 "left" / "right"。金额与数量右对齐（数字列左对齐读起来会错行）。
	Align string
}

// TableData 表格。
type TableData struct {
	Columns []Column
	// Rows 每行一个切片，元素数应与 Columns 一致；不齐时渲染器按列数截断或补空，
	// **不报错**（数据源已经把数据查出来了，为一行少一个格子废掉整张表不合算）。
	Rows [][]string
	// Empty 没有行时的提示语（不给就渲染一行「无数据」）。
	Empty string
}

// Item 列表 / 手风琴里的一项。
type Item struct {
	Label string
	// Value 值（可为空：纯说明性列表不需要值）。
	Value string
	// Hint 补充说明（小字，可为空）。
	Hint string
}

// ListData 列表。
type ListData struct {
	Items []Item
	Empty string
}

// Stat 一个指标卡。
type Stat struct {
	Label string
	Value string
	// Delta 涨跌文案（如 "+12.4%"），可为空。
	Delta string
	// Trend 涨跌方向："" / "up" / "down" / "flat"。只用于配色，不参与判断：
	// 「涨了是好事还是坏事」（退款率涨了是坏事）是业务判断，不归渲染器管。
	Trend string
}

// StatData 指标卡组。
type StatData struct {
	Items []Stat
	// Empty 没有指标时的提示语。
	Empty string
}

// Group 手风琴的一组。
type Group struct {
	Title string
	// Body 正文（纯文本，可为空）。
	Body string
	// Items 组内的键值条目（可为空）。Body 与 Items 都空时该组不渲染。
	Items []Item
}

// AccordionData 手风琴。
type AccordionData struct {
	Groups []Group
	Empty  string
}
