package scoring

// title_unique.go — 编辑期 title 唯一性检查（审计 SEO-018 的轻量版）。
//
// 与发布侧体检的分工：
//   - 发布侧（internal/module/publication/service/seo_audit.go 的 AuditTitleDuplicate）
//     读的是**激活产物**里真正服务出去的 <title> —— 模板把标题渲染成了什么、
//     有没有被多语言/品牌后缀拼过，只有发布之后才知道；
//   - 编辑期（本文件）比的是**实体字段**（实体 SEO 标题 / 页面草稿的 settings.seo.title），
//     写之前就能提示，不必等一次发布。
//
// 两处的结论形状**刻意保持一致**：列出全部命中页面，而不是只说「有重复」。
// 只报数量的话运营知道有问题却不知道该去改哪一页 —— SEO-019 实现时踩过一次
// 这个坑（见该审计条目的 resolutionNote 第 ⑦ 条），这里不再踩第二次。

import (
	"sort"
	"strings"
)

// TitleEntry 一条已存在的 title 及其归属页面。
type TitleEntry struct {
	Title string // 已存在的标题（比较用原文）
	Page  string // 归属页面的可读标识（优先线上路径 / 后台路径，其次实体名）
	ID    string // 实体 id（用于排除自身；空表示无法排除）
}

// DuplicateTitles 返回与 target 重复的**其它**页面（无重复返回 nil）。
//
// 比较口径与发布侧 AuditSite 对齐：先 TrimSpace，再精确比较 —— 发布侧是按解析出来的
// <title> 文本建 map 的，大小写不同的标题在那边本来就不算重复。这里若改成忽略大小写，
// 会出现「草稿期报重复、发布后体检不报」的两套结论，运营只会认为检查不可信。
//
// target 为空直接返回 nil：没填标题是 title_present 那条检查的事，
// 把它算成「所有空标题页面互相重复」只会刷出一屏无意义的冲突。
func DuplicateTitles(target string, entries []TitleEntry, selfID string) []TitleEntry {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []TitleEntry
	for _, e := range entries {
		if selfID != "" && e.ID != "" && e.ID == selfID {
			continue
		}
		if strings.TrimSpace(e.Title) != target {
			continue
		}
		page := strings.TrimSpace(e.Page)
		if page == "" {
			page = strings.TrimSpace(e.ID)
		}
		// 同一个页面在索引里出现两次（例如同时登记了页面与它发布的实例）时不重复列出。
		if page == "" || seen[page] {
			continue
		}
		seen[page] = true
		out = append(out, TitleEntry{Title: target, Page: page, ID: e.ID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Page < out[j].Page })
	return out
}

// DuplicateTitleMessage 冲突结论的一句话说明（与发布侧同一句式）。
//
// 句式统一是刻意的：运营在编辑期与发布后体检看到的是同一句话，才能确认
// 「这是同一类问题」，而不是两个功能各说各话。
func DuplicateTitleMessage(title string, dups []TitleEntry) string {
	if len(dups) == 0 {
		return ""
	}
	pages := make([]string, 0, len(dups))
	for _, d := range dups {
		pages = append(pages, d.Page)
	}
	return "重复的 title（" + strings.TrimSpace(title) + "）出现在 " + itoa(len(pages)) + " 个页面：" + strings.Join(pages, "、")
}
