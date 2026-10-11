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
//
// 本文件现在有**两层**（迁移期并存，见 task-94 设计）：
//   - 上层的 BuildPagination/PaginationData 是**展示产物**（信息行句子、按钮文字、href、
//     选中态、省略号都在 Go 侧预制成成品），正在被逐批替换；
//   - 末段「事实层」的 Page/Facts 是终局替代：只回答「值是多少」，显示成什么样由模板决定
//     （docs/rules/template-boundary.md：模板回答显示成什么样，不回答值是多少）。
//     旧 API 的删除（含本段上方那些类型）属于独立的最后一步，不与新增混做。
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

// ============================ 事实层 ============================
//
// 下面这些是分页的「值是多少」，与上面那层的分工是**事实 vs 展示**：
//
//   - 事实：总数、当前页、每页条数、共几页、本页覆盖哪一段、页码参数叫什么、
//     链接里要不要带 limit、链接基址（路径 + 已知筛选）。
//   - 不在这里：信息行句子（「共 %s 条，第 %s-%s 条」）、上/下一页文字、href 串接、
//     选中/禁用态、省略号画在哪 —— 这些由模板用通用函数做（internal/templates/funcs.go
//     的 sprintf/queryURL/pageNumbers）。
//
// 三条必须知道的约定（都是踩过才写下来的，别按直觉改）：
//
//  1. **值语义**。Facts 返回 Page **值**，零值表示「没有分页」。这不是风格选择：
//     探针实测（internal/templates/admin_pagination_probe_p0_test.go）指针/类型化 nil 一旦
//     穿透模板守卫就是进程级 panic，而值类型的零值只会让 `{{if .HasPagination()}}` 为假。
//  2. **默认参数与现行链接字节一致**：PageParam 默认 "page"、KeepLimit 默认 true，即
//     `?page=N&limit=M` —— 迁移期间 31 个调用点的链接必须一字不变。唯一例外是 mail 的
//     账号页（参数名 account_page 且不保留 limit），由调用点显式覆盖这两个字段。
//  3. **本段零依赖**：不 import fmt/strconv/i18n，也不拼任何文案或 URL。一旦在这里拼句子，
//     展示就又搬回 Go 了 —— pagination_facts_test.go 里有边界守卫断言盯住这一点。

// Page 是分页的**事实**：在第几页、每页多少条、共几页、本页覆盖哪一段。
//
// PageParam/KeepLimit/Base 由调用点补齐（Facts 给出与现状一致的默认值）。
type Page struct {
	Total int64
	Page  int
	Limit int
	Pages int
	Start int // 本页第一条的序号（1 起）；没有数据时为 0
	End   int // 本页最后一条的序号；没有数据时为 0

	PageParam string // 页码参数名（默认 "page"）
	KeepLimit bool   // 链接里是否带 limit（默认 true，与现行链接一致）
	Base      string // 链接基址：页面路径 + 已知筛选条件（不含 page/limit）
}

// Facts 把「总数 / 当前页 / 每页条数」收敛成分页事实。
//
// 夹紧与取整口径与 BuildPagination 逐字一致（limit 下限 20、page 下限 1、页数向上取整、
// page 超出页数时夹到末页），因为迁移期的验收判据就是「渲染字节相同」。
func Facts(total int64, page, limit int) Page {
	if limit < 1 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(limit) - 1) / int64(limit))
	}
	if pages > 0 && page > pages {
		page = pages
	}
	p := Page{
		Total:     total,
		Page:      page,
		Limit:     limit,
		Pages:     pages,
		PageParam: "page",
		KeepLimit: true,
	}
	if total > 0 {
		p.Start = (page-1)*limit + 1
		p.End = page * limit
		if int64(p.End) > total {
			p.End = int(total)
		}
	}
	return p
}

// HasPagination 是否该显示分页条：多于一页才有意义（单页与空集都不渲染）。
func (p Page) HasPagination() bool { return p.Pages > 1 }

// HasPrev / HasNext 决定上/下一页是链接还是禁用态。
func (p Page) HasPrev() bool { return p.Page > 1 }
func (p Page) HasNext() bool { return p.Page < p.Pages }

// PrevPage / NextPage 只在 HasPrev/HasNext 为真时使用（模板里就是这么写的）。
func (p Page) PrevPage() int { return p.Page - 1 }
func (p Page) NextPage() int { return p.Page + 1 }
