package pagination

// pagination.go — 分页条数据（服务端渲染，零 JS）。
//
// 归属 pkg 而不是 internal/shell：这里是纯数据变换（把 total/page/limit 变成页码按钮
// 列表 + 信息文案），没有 gin、没有模板、没有 i18n 状态。放在 pkg 的好处有两条：
//   - handler 直接用，不引入后台外壳的依赖；
//   - 模板侧也能把它注册成模板函数 —— 不受「internal/shell 反向依赖 internal/templates」
//     那条环的约束（templates 不能 import shell，但可以 import pkg/pagination）。
//
// 注意这里**不做 SQL 分页**：总数与当页数据由 model 层用 GORM 取（Count + Offset/Limit）。
// 本包只负责「分页条长什么样」的渲染数据。
//
// 链接为普通 GET 参数（?page=2&limit=20），符合项目「只用 GET/POST + Query 参数」约定；
// 点页码即整页刷新，无需前端状态，也不与抽屉/筛选脚本耦合。
import (
	"fmt"
	"strconv"
)

// PageLink 分页条上的单个链接（页码 / 上一页 / 下一页 / 省略号）。
type PageLink struct {
	Label    string
	URL      string
	Active   bool
	Disabled bool
}

// PaginationData 分页数据；Total 为 0 或单页时 BuildPagination 返回 nil，模板据此不渲染分页条。
type PaginationData struct {
	Total int64
	Page  int
	Limit int
	Info  string // 「共 119 条，第 1-20 条」
	Links []PageLink
}

// MaxPageLinks 页码按钮最大数量（超出用省略号）。
const MaxPageLinks = 7

// TemplateKeys 返回模板用的扁平键（避免 Jet 对「索引 + 点号」链式访问的不确定性）。
// 无分页时返回空 map，模板的 {{if}} 自然跳过。
func (p *PaginationData) TemplateKeys() map[string]any {
	if p == nil {
		return map[string]any{}
	}
	return map[string]any{
		"PaginationInfo":  p.Info,
		"PaginationLinks": p.Links,
	}
}

// BuildPagination 构造分页数据；total 为 0 或单页时返回 nil。
//
// baseURL 为不含 page/limit 的页面路径（如 /admin/permissions），其余查询参数由调用方
// 自行拼进 baseURL（例如 /admin/themes/settings?id=xxx）。
//
// t 为按请求语言取文案的翻译函数（签名同模板层 t(key, fallback)）；传 nil 时退回中文原文。
func BuildPagination(total int64, page, limit int, baseURL string,
	t func(key, fallback string) string) *PaginationData {
	if t == nil {
		t = func(_, fallback string) string { return fallback }
	}
	if limit < 1 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages <= 1 {
		return nil
	}
	if page > pages {
		page = pages
	}

	start := (page-1)*limit + 1
	end := page * limit
	if int64(end) > total {
		end = int(total)
	}

	link := func(p int) string {
		sep := "?"
		for _, c := range baseURL {
			if c == '?' {
				sep = "&"
				break
			}
		}
		return fmt.Sprintf("%s%spage=%d&limit=%d", baseURL, sep, p, limit)
	}

	// 文案走 sys_i18n（shell.pagination.*）；占位符统一用 %s（与 pkg/i18n 的
	// HasStringPlaceholdersOnly 约定一致，数字在 Go 侧先转字符串），缺词条回退中文原文。
	pd := &PaginationData{
		Total: total,
		Page:  page,
		Limit: limit,
		Info: fmt.Sprintf(t("shell.pagination.info", "共 %s 条，第 %s-%s 条"),
			strconv.FormatInt(total, 10), strconv.Itoa(start), strconv.Itoa(end)),
	}

	prevLabel := t("shell.pagination.prev", "上一页")
	nextLabel := t("shell.pagination.next", "下一页")

	// 上一页
	if page > 1 {
		pd.Links = append(pd.Links, PageLink{Label: prevLabel, URL: link(page - 1)})
	} else {
		pd.Links = append(pd.Links, PageLink{Label: prevLabel, Disabled: true})
	}

	// 页码：窗口内连续显示，首尾固定，超出用省略号（不可点）。
	win := MaxPageLinks
	if pages <= win {
		for p := 1; p <= pages; p++ {
			pd.Links = append(pd.Links, PageLink{Label: strconv.Itoa(p), URL: link(p), Active: p == page})
		}
	} else {
		half := (win - 3) / 2 // 预留首、尾、当前页
		lo := page - half
		hi := page + half
		if lo < 2 {
			lo, hi = 2, lo+win-3
		}
		if hi > pages-1 {
			hi, lo = pages-1, pages-1-(win-3)
		}
		pd.Links = append(pd.Links, PageLink{Label: "1", URL: link(1), Active: page == 1})
		if lo > 2 {
			pd.Links = append(pd.Links, PageLink{Label: "…", Disabled: true})
		}
		for p := lo; p <= hi; p++ {
			if p < 2 || p > pages-1 {
				continue
			}
			pd.Links = append(pd.Links, PageLink{Label: strconv.Itoa(p), URL: link(p), Active: p == page})
		}
		if hi < pages-1 {
			pd.Links = append(pd.Links, PageLink{Label: "…", Disabled: true})
		}
		pd.Links = append(pd.Links, PageLink{Label: strconv.Itoa(pages), URL: link(pages), Active: page == pages})
	}

	// 下一页
	if page < pages {
		pd.Links = append(pd.Links, PageLink{Label: nextLabel, URL: link(page + 1)})
	} else {
		pd.Links = append(pd.Links, PageLink{Label: nextLabel, Disabled: true})
	}
	return pd
}
