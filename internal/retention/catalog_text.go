package retention

// catalog_text.go — 声明表的**文本读出口**（CLI / 运维手册用）。
//
// 为什么需要它：包注释写着「运维手册与后台展示都从这里取」，但此前没有任何读出口 ——
// 唯一消费者是 catalog_test.go，等于那句话没有兑现（要读声明只能翻源码）。
//
// 形态选 CLI 而不是后台页：声明是**静态数据**（不需要数据库、不需要权限点、不需要模板），
// 一个只读命令就是成本最低的读出口；后台页要牵进路由 + 权限点 + 模板三处改动，
// 而它回答的问题（「这张表留多久、谁在清、为什么」）运维本来就在终端里问。
// 入口见 `cmd/retention-catalog`。

import (
	"fmt"
	"strings"
	"time"
)

// FormatDeclarations 把全部声明渲染成人类可读的文本（CLI 直接打印）。
//
// 字段顺序按「先看结论」排：表名 → 清理方式 → 保留期 → 时间列 → 执行者 → 理由。
// 输出必须**包含每一条声明**（CLI 的价值就是「一张不漏地看到」），
// 所以这里不做任何过滤或截断。
func FormatDeclarations() string {
	list := Declarations()
	var b strings.Builder
	fmt.Fprintf(&b, "数据生命周期声明（共 %d 张表；真源 internal/retention/catalog.go）\n", len(list))
	b.WriteString("清理方式：" + string(CleanupDelete) + " 按时间分批删 / " +
		string(CleanupOrphan) + " 按引用清孤儿 / " + string(CleanupNone) + " 不清理\n\n")
	for _, d := range list {
		fmt.Fprintf(&b, "· %s\n", d.Table)
		fmt.Fprintf(&b, "    清理方式：%s\n", d.Kind)
		fmt.Fprintf(&b, "    保留期：%s\n", formatRetain(d))
		if col := strings.TrimSpace(d.TimeColumn); col != "" {
			fmt.Fprintf(&b, "    时间列：%s\n", col)
		}
		if ex := strings.TrimSpace(d.Executor); ex != "" {
			fmt.Fprintf(&b, "    执行者：%s\n", ex)
		}
		fmt.Fprintf(&b, "    说明：%s\n", d.Note)
	}
	return b.String()
}

// formatRetain 保留期的人类可读形态。
//
// 「不清理」与「0 天」必须分开表达：CleanupDelete 且 Retain=0 的条目是**保留期由运行时
// 配置决定**（唯一一条是 page_views，按工程可配），打印成「0 天」读起来像「立刻全删」——
// 那是这张表最不该制造的误解。
func formatRetain(d Declaration) string {
	if d.Retain <= 0 {
		if d.Kind == CleanupDelete {
			return "0（由运行时配置决定，见说明）"
		}
		return "不清理"
	}
	if days := d.Retain / (24 * time.Hour); days*24*time.Hour == d.Retain {
		return fmt.Sprintf("%d 天", days)
	}
	return d.Retain.String()
}
