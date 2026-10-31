package retention

import (
	"fmt"
	"strings"
	"time"
)

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
