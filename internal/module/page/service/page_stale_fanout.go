package pageservice

// page_stale_fanout.go — 逐工程扇出的**命中集合聚合**（写侧）。
//
// 背景：本模块一批「整站标记／精确标记」入口的形状都是「枚举工程 → 逐工程独立作用域执行
// → 合并命中集合」（为什么要逐工程见 page_scope.go）。合并这一段此前在每个入口里各写一遍
// 内联循环；本次给整站标记（主题 / 块 / 词条）接影响面回执时若不收口，它就会变成第四、
// 第五份 —— 去重抄漏一处，日志里的「本次影响 N 个页面」就会比实际多。
//
// 本文件只做聚合：命中集合由 model 的 RETURNING id 给出，聚合结果交给
// page_stale_overview.go 的 logStaleImpact 记影响面。不碰数据库、不做业务判断。

import "strings"

// staleIDCollector 累积逐工程扇出命中的页面 id：去重、保持首次出现次序、跳过空白 id。
//
// 为什么去重是防御性的而不是必需的：pages.id 是主键，每个工程的 UPDATE 都带 project_id
// 条件，同一个 id 不可能被两个工程同时命中。留着它是因为「只有一个实现」才是真正要守的
// 东西 —— 它同时是「本次影响多少个页面」这个数字的唯一来源（写侧聚合与日志归一化都走它）。
//
// 零值可用（seen 懒初始化），便于在循环外直接声明。
type staleIDCollector struct {
	seen map[string]bool
	ids  []string
}

// add 合并一次扇出的命中集合（空集合是常态，直接返回）。
//
// id 按 TrimSpace 后的值收录，与 StaleImpactOfIDs 的归一化口径一致：两处口径不同的表现是
// 「日志里的总数」与「摘要里的总数」对不上，而这两个数都只在日志里看得见。
func (c *staleIDCollector) add(hit []string) {
	for _, id := range hit {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if c.seen == nil {
			c.seen = make(map[string]bool, len(hit))
		}
		if c.seen[id] {
			continue
		}
		c.seen[id] = true
		c.ids = append(c.ids, id)
	}
}

// list 返回累积的 id 集合。一个都没有时返回 nil —— 调用方按 len 判断即可：
// 这里刻意不返回「空但非 nil」的切片去逼调用方区分两者，那区分没有语义。
func (c *staleIDCollector) list() []string { return c.ids }
