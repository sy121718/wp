package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 空态容器只能有一份形态：admin/partials/empty_state.html。
//
// 判据是**处数**（双向）：后台模板里手写的 <div class="empty-state"> 只能减少 ——
// 新写一处就失败，改完一处忘了下调预算也失败。这是「同一形态只写一遍」唯一能自动守住的形式。
//
// 2026-10 由 69 降到 68（78bd5e0f 在 analytics/seo.html 一次加了 3 处、同时 2 处转成片段）：
// 新增的 placeholder / empty 两档纯属「标题 + 说明」→ 改成 include；clean 一档仍手写，
// 因为标题里嵌 <span data-seo-audit-scanned> 由 JS 回填计数，而片段的 title 只收已取好词的
// 整句、塞不了可变节点（见该行上方注释）；top-paths 卡片的空态带「去访问统计」动作，
// 片段支持 actionHref/actionLabel、与一次写死的 href 等价 → 也转成 include。
const emptyStateBudget = 68

const emptyStatePartial = "internal/templates/admin/partials/empty_state.html"

// 状态徽标同理：形态只在 admin/partials/badge.html 里，模板里手写的 badge 只能减少。
// 只数 <span class="badge badge-…">（td/div 上的 badge 是表格与卡片自己的类，不是这个形态）。
//
// 2026-10 定时上下线（PIPE-7）下线时从 163 降到 160（-3：
// pages.html 的两个排定徽标 + 面板片段的空态徽标随文件一起删掉），
// 预算只减不增，所以这里同步下调 —— 留在 163 会让「已经收口」与「一点没改」在清单上长得一样。
//
// 2026-10 由 154 降到 151（78bd5e0f 在 analytics/seo.html 加 4 处、本轮收口 7 处）：
// 78bd5e0f 的手写徽标里，级别四档（error/warning/info/missing）改为模板只给
// data-seo-badge-tone token、由 seo-audit.js 的 levelBadge 拼出 badge.html 的同一形态；
// 余下 5 处是 `<p class="badge badge-…">` 告警条 —— 元素是 <p>、带 role=alert，
// 形态与 badge.html（<span> + 文案）不同，仍保留手写（已在该页注释里写明）。
// 另有 4 处「只有一个变量 + 固定 tone」的徽标收口成 include：
// masterdata_changes.html、block_row.html、product_publish_records.html（语言列）、
// dashboard.html —— 这四处原本就是 badge.html 的完全等价形态，只差一次 include。
const badgeBudget = 151

const badgePartial = "internal/templates/admin/partials/badge.html"

func TestEmptyStateMarkupStaysInPartial(t *testing.T) {
	partial, err := os.ReadFile(filepath.Join(repoRoot(t), emptyStatePartial))
	if err != nil {
		t.Fatalf("空态片段读不到（后台唯一的空态形态就是它）: %v", err)
	}
	if !strings.Contains(string(partial), `<div class="empty-state" role="status">`) {
		t.Fatal("空态片段必须是容器本身：<div class=\"empty-state\" role=\"status\">（读屏用户靠 role 得知零结果）")
	}

	count := 0
	files := 0
	err = filepath.WalkDir(filepath.Join(repoRoot(t), "internal/templates/admin"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		if strings.HasSuffix(filepath.ToSlash(path), emptyStatePartial) {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		n := strings.Count(string(body), `<div class="empty-state"`)
		if n > 0 {
			files++
			count += n
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}
	if count != emptyStateBudget {
		t.Fatalf("手写空态容器 %d 处（%d 个文件），预算 %d 处：\n"+
			"· 多出来 = 新写了一处空态 → 改用 {{include \"../partials/empty_state.html\" map(\"title\", …)}}\n"+
			"· 变少 = 已经收口 → 把本文件的 emptyStateBudget 改成 %d（预算只减不增）",
			count, files, emptyStateBudget, count)
	}
}

// TestBadgeMarkupStaysInPartial 同上：后台模板里的 <span class="badge badge-…"> 只能减少。
//
// tone（配色档位）由模板按状态选，服务端不给 Tone / Class 字段；形态与色调 token 只写一份。
func TestBadgeMarkupStaysInPartial(t *testing.T) {
	partial, err := os.ReadFile(filepath.Join(repoRoot(t), badgePartial))
	if err != nil {
		t.Fatalf("徽标片段读不到: %v", err)
	}
	if !strings.Contains(string(partial), `<span class="badge badge-{{.tone}}">{{.label}}</span>`) {
		t.Fatal("徽标片段必须是唯一形态：<span class=\"badge badge-{{.tone}}\">{{.label}}</span>")
	}

	count := 0
	err = filepath.WalkDir(filepath.Join(repoRoot(t), "internal/templates/admin"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") || strings.HasSuffix(filepath.ToSlash(path), badgePartial) {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		count += strings.Count(string(body), `<span class="badge badge-`)
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}
	if count != badgeBudget {
		t.Fatalf("手写徽标 %d 处，预算 %d 处：\n"+
			"· 多出来 = 新写了一处徽标 → 改用 {{include \"../partials/badge.html\" map(\"tone\", …)}}\n"+
			"· 变少 = 已经收口 → 把本文件的 badgeBudget 改成 %d（预算只减不增）",
			count, badgeBudget, count)
	}
}
