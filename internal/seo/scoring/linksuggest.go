// linksuggest.go — 内链建议引擎（SEO-015）：候选过滤 + 重叠度排序（纯函数、无 IO）。
//
// 定位与评分器本体一致：输入由调用方装配（候选列表来自「已发布、有真实路径」的端口，
// 本包不查库、不验发布状态），这里只做过滤与排序。语义相似度明确不做 —— 重叠度
// 只看三个可解释的信号：focusKeyword 命中、标签交集、分类交集。
package scoring

import (
	"sort"
	"strings"
)

// LinkCandidate 内链候选（一条 = 一个已发布且有真实路径的内容项）。
type LinkCandidate struct {
	ID           string   // 实体 id（用于排除自身）
	Title        string   // 建议展示的标题
	URL          string   // 线上路径（空 = 没有真实路径，调用方本就不该放进候选，这里兜底再滤一次）
	FocusKeyword string   // 该内容的焦点关键词
	Tags         []string // 标签（文章域当前没有，留作扩展；接口先固定）
	Categories   []string // 分类
}

// LinkDocument 当前正在编辑的文档上下文。
type LinkDocument struct {
	SelfID       string          // 当前内容 id（与候选 ID 同一命名空间）
	SelfURL      string          // 当前内容的线上路径（空则不做自身 URL 排除）
	FocusKeyword string          // 当前焦点关键词
	Tags         []string        // 当前标签
	Categories   []string        // 当前分类
	LinkedURLs   map[string]bool // 正文已有内链的目标 URL 集合（含自身路径由调用方决定）
}

// LinkSuggestion 一条内链建议（点击即可拿到可插入的标题 + URL）。
type LinkSuggestion struct {
	ID     string
	Title  string
	URL    string
	Score  int    // 重叠度得分：关键词命中 3、标签每交集 +2、分类每交集 +1
	Reason string // 得分来源的简述（空 = 仅按路径推荐，无重叠信号）
}

// 内链建议的各项权重：关键词命中权重最高（它直接对应搜索意图），
// 标签次之（主题相关性），分类最弱（桶大、信号稀）。
const (
	linkScoreKeyword  = 3
	linkScoreTag      = 2
	linkScoreCategory = 1
)

// InternalLinkSuggestions 过滤候选并按重叠度排序。
//
// 过滤规则（按序）：
//  1. URL 为空的候选直接丢弃 —— 内链建议绝不能产生死链，没有真实路径就没有资格；
//  2. 排除自身（ID 相同，或 URL 与 SelfURL 相同 —— 两道闸是冗余防护，id 体系不同的
//     调用方至少还能靠 URL 挡住自链）；
//  3. 排除正文已链接的目标（LinkedURLs 命中 URL）。
//
// 排序规则：得分降序；并列时按 Title 升序（确定性输出，同一次请求两次调用结果一致）。
// limit <= 0 表示不截断。doc 与候选里的 nil 安全：nil 文档返回空。
func InternalLinkSuggestions(doc *LinkDocument, candidates []LinkCandidate, limit int) []LinkSuggestion {
	if doc == nil || len(candidates) == 0 {
		return nil
	}
	keyword := strings.ToLower(strings.TrimSpace(doc.FocusKeyword))
	linked := doc.LinkedURLs
	selfURL := strings.TrimSpace(doc.SelfURL)

	out := make([]LinkSuggestion, 0, len(candidates))
	for _, cand := range candidates {
		cand.URL = strings.TrimSpace(cand.URL)
		if cand.URL == "" {
			continue // 没有真实路径的候选没有资格成为内链（死链防线，双保险）
		}
		if strings.TrimSpace(cand.ID) != "" && cand.ID == doc.SelfID {
			continue
		}
		if selfURL != "" && cand.URL == selfURL {
			continue
		}
		if linked[cand.URL] {
			continue
		}
		score, reason := linkOverlapScore(keyword, doc.Tags, doc.Categories, cand)
		out = append(out, LinkSuggestion{
			ID: cand.ID, Title: cand.Title, URL: cand.URL,
			Score: score, Reason: reason,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Title < out[j].Title
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// linkOverlapScore 计算单个候选与当前文档的重叠度得分与来源简述。
func linkOverlapScore(keyword string, docTags, docCategories []string, cand LinkCandidate) (score int, reason string) {
	var parts []string
	candKeyword := strings.ToLower(strings.TrimSpace(cand.FocusKeyword))
	if keyword != "" && candKeyword != "" && (keyword == candKeyword || strings.Contains(keyword, candKeyword) || strings.Contains(candKeyword, keyword)) {
		score += linkScoreKeyword
		parts = append(parts, "关键词匹配")
	}
	if n := overlapCount(docTags, cand.Tags); n > 0 {
		score += n * linkScoreTag
		parts = append(parts, "标签重叠")
	}
	if n := overlapCount(docCategories, cand.Categories); n > 0 {
		score += n * linkScoreCategory
		parts = append(parts, "分类重叠")
	}
	return score, strings.Join(parts, "、")
}

// overlapCount 两个字符串集合的交集数（trim + 忽略大小写，空串不参与）。
func overlapCount(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]bool, len(b))
	for _, v := range b {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			set[v] = true
		}
	}
	n := 0
	for _, v := range a {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" && set[v] {
			n++
		}
	}
	return n
}
