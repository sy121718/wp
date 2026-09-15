// Package money 金额格式化的唯一实现（审计 CQ-013）。
//
// 项目里金额文本有两种口径，消费方不同、要求相反，所以这里给出显式命名的函数，
// 而不是一个「万能格式化」—— 合并成一个必然踩到其中一侧：
//
//   - FormatAudit：写入审计快照 / 比较前后值的口径，**固定两位小数**。主数据变更表
//     逐字段记 old/new，同一金额的两种表示（"99" 与 "99.00"）会被判成一次变更，
//     留下「值没变但审计多了一条」的假记录，所以这里必须是定长文本。
//   - FormatYuan：展示口径，**最短表示**（整数不带小数尾巴）。构建产物、运行时片段、
//     后台页面读到的都是它；产物里的价格字节与这里的输出必须一致，一旦补零，
//     同一个价在页面上会「看起来变了」。
//   - CentsToYuanText：分 → 元展示文本（换算 + FormatYuan 口径）。数量的真源是
//     整数分，元只存在于展示层。
//
// 本包只做格式化，不含舍入策略与货币换算：金额运算一律用整数分（见事务一致性维度）。
package money

import "strconv"

// FormatAudit 金额（numeric(12,2)，单位元）→ 固定两位小数的字符串。
//
// 用于审计快照与前后值比较：定长文本才谈得上「两个值是否相同」。
func FormatAudit(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// FormatYuan 金额（单位元）→ 展示文本：整数不带小数尾巴，其余按最短表示。
func FormatYuan(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// CentsToYuanText 分 → 元展示文本，与 FormatYuan 同一口径（9950 分 → "99.5"）。
func CentsToYuanText(cents int64) string {
	return FormatYuan(float64(cents) / 100)
}
