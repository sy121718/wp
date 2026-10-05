// Package prompt 提供 AI 请求里**稳定前缀**的那一段内容。
//
// 为什么单独成一个包：这段文本要被**单测直接断言**（同一会话两次请求的前缀逐字节一致，
// docs/16 §3），而 service 包依赖数据库与上游 HTTP —— 放这里可以让断言只依赖一个字符串。
//
// 稳定性纪律（改这个包之前先读 docs/16 §3）：
//   - 内容**只能追加尾部**，不要在中间插行、不要改措辞、不要换分隔符；
//   - 禁止写入任何随时间变化的内容（日期、"最新"、账号名、请求 id）——
//     这类内容会让 provider 侧的前缀缓存每一轮都整段作废，而症状只是账单变贵；
//   - 改动顺序（例如把两段对调）等价于改动整段前缀，同样要评估缓存损失。
package prompt

import (
	"embed"
	"sort"
	"strings"

	_ "embed"
)

// siteRules 常驻规则（口径、术语、红线）。内容来自 site_rules.md，编译进二进制。
//
// 用 go:embed 而不是读文件：部署形态里工作目录不一定有 docs，而"规则读不到"的降级
// 只能是"没有规则"—— 那比编译失败更糟（模型会按自己的常识回答金额与口径）。
//
//go:embed site_rules.md
var siteRules string

// manuals 各领域的手册（口径与注意事项）。
//
// 用 embed.FS 而不是一个个 //go:embed 变量：文件是后来逐个加的，一个个声明会在
// 「加了文件忘了声明」时静默少一片 —— 而少一片手册的症状是「模型对某个领域按常识回答」，
// 没有任何报错。
//
//go:embed manual/*.md
var manuals embed.FS

// ManualCatalog 手册目录（名字 + 一句话），进稳定前缀的第二段。
//
// **从文件本身生成，不另写一份清单**：手写的目录会在加/改手册时漂移，
// 而漂移的方向恰好是最坏的那种 —— 目录里列着一个已经改名的主题（模型去取，取不到），
// 或者新加的主题不在目录里（模型不知道它存在）。
//
// 每行取的是手册**标题行**（`# 销售（订单）` 里的 `销售（订单）`），
// 而不是文件名：文件名是英文标识符，模型拿它当话题名会在回答里露出来。
func ManualCatalog() string {
	entries := ManualTopics()
	if len(entries) == 0 {
		return ""
	}
	names := make([]string, 0, len(entries))
	entries2 := make(map[string]string, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
		entries2[e.Name] = e.Title
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("可用手册（需要其中某个领域的口径细节时，用 guide 工具按名字取全文）：\n")
	for _, n := range names {
		b.WriteString("- ")
		b.WriteString(n)
		b.WriteString("：")
		b.WriteString(entries2[n])
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ManualTopic 一篇手册的目录项。
type ManualTopic struct {
	// Name 取用名（文件名去后缀），也是 guide 工具的参数值。
	Name string
	// Title 标题行去掉 `# ` 前缀后的文本。
	Title string
}

// ManualTopics 列出全部手册（按名字升序，顺序稳定 —— 它进稳定前缀）。
func ManualTopics() []ManualTopic {
	dir, err := manuals.ReadDir("manual")
	if err != nil {
		return nil
	}
	out := make([]ManualTopic, 0, len(dir))
	for _, f := range dir {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(f.Name(), ".md")
		title := name
		if raw, err := manuals.ReadFile("manual/" + f.Name()); err == nil {
			title = firstHeading(string(raw), name)
		}
		out = append(out, ManualTopic{Name: name, Title: title})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// firstHeading 取首行 `# xxx` 的 xxx；没有标题行时回退到 fallback（文件名）。
func firstHeading(text, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		// 首个非空行不是标题：说明这篇文件的形态不是我们约定的那种，
		// 用文件名兜底而不是把它整行当标题（那一行可能是正文，塞进目录会很难看）。
		break
	}
	return fallback
}

// Manual 取一篇手册全文。名字不存在时返回 ("", false)。
//
// 消费者（guide 工具）据此回一句「没有这篇手册，可用的是：…」——
// **不能**返回空串当成功，那会让模型以为「这个领域没有口径」而按常识回答。
func Manual(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/\\.") {
		return "", false
	}
	raw, err := manuals.ReadFile("manual/" + name + ".md")
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// SiteRules 返回常驻规则全文（稳定前缀的第一段）。
//
// 返回的是**常量内容**：同一进程内每次调用逐字节一致。调用方直接放进 system 消息，
// 不要在前面拼时间戳或用户名 —— 那会破坏前缀稳定性（这一条有测试钉住）。
func SiteRules() string {
	return siteRules
}
