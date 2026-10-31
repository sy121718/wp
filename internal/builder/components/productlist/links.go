package productlist

import (
	json "go_wp/pkg/jsonx"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/seo"
)

const catalogPath = "/api/site/product/catalog"

type linkContext struct {
	instanceQuery string
	pushQuery     string

	pageURL string
}

func newLinkContext(nodeID string, p *Props, ctx *core.RenderContext) linkContext {
	semantic := url.Values{}
	for key, value := range collectionFilter(p) {
		if key != filterKeyStatus {
			semantic.Set(key, value)
		}
	}
	semantic.Set("orderBy", effectiveOrder(p))
	semantic.Set("pageSize", strconv.Itoa(EffectivePageSize(p)))
	semantic.Set("layout", effectiveLayout(p))
	semantic.Set("columns", effectiveColumns(p))
	for key, value := range parseQuery(p.PushQuery) {
		semantic[key] = value
	}
	lc := linkContext{instanceQuery: catalogQuery(nodeID, p, ctx), pushQuery: semantic.Encode()}
	if ctx != nil {
		lc.pageURL = seo.AbsoluteSiteURL(strings.TrimSpace(ctx.CurrentPath))
	}
	return lc
}

func (c linkContext) pushURL(override url.Values) string {
	values := url.Values{}
	for k, v := range parseQuery(c.pushQuery) {
		values[k] = v
	}
	for k, v := range override {
		if len(v) == 0 || strings.TrimSpace(v[0]) == "" {
			values.Del(k)
			continue
		}
		values[k] = v
	}
	if encoded := values.Encode(); encoded != "" {
		return "?" + encoded
	}
	return "?"
}

func (c linkContext) fallbackHref(override url.Values) string {
	q := c.pushURL(override)
	if c.pageURL == "" {
		return q
	}
	if q == "?" {
		return c.pageURL
	}
	return c.pageURL + q
}

func parseQuery(raw string) url.Values {
	raw = strings.Trim(strings.TrimSpace(raw), "&")
	if raw == "" {
		return url.Values{}
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return url.Values{}
	}
	return values
}

func catalogQuery(_ string, p *Props, ctx *core.RenderContext) string {
	q := url.Values{}
	if ctx != nil {
		q.Set("projectId", ctx.ProjectID)
		if ctx.Lang != "" {
			q.Set("lang", ctx.Lang)
		}
	}
	filter := collectionFilter(p)
	encoded, _ := json.Marshal(filter)
	q.Set("filter", string(encoded))
	q.Set("sort", effectiveOrder(p))
	q.Set("page", strconv.Itoa(EffectivePage(p)))
	size := EffectivePageSize(p)
	if size == 0 {
		size = effectiveLimit(p)
		if size > MaxPageSize {
			size = MaxPageSize
		}
	}
	q.Set("pageSize", strconv.Itoa(size))
	return q.Encode()
}

func propOrEmpty(p *Props, pick func(*Props) string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(pick(p))
}

func optionFilterKeys(p *Props) []string {
	if p == nil {
		return nil
	}
	keys := make([]string, 0, 4)
	for _, pair := range strings.Split(p.FilterOptions, ",") {
		key, _, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok {
			continue
		}
		if key = strings.TrimSpace(key); key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
