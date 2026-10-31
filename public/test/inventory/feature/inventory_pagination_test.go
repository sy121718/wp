package feature

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

const paginationBar = `class="pagination"`

func paginationLinkLine(t *testing.T, body string, page int) string {
	t.Helper()
	needle := "page=" + strconv.Itoa(page)
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) && strings.Contains(line, "<a ") {
			return line
		}
	}
	t.Fatalf("分页条里没有指向第 %d 页的链接（分页条没渲染？还是链接里没有 page=%d？）", page, page)
	return ""
}

func TestInventoryMovementsPagePagination(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	p := mustProduct(t, f, "流水分页商品")
	v := f.firstVariant(t, p.ID)

	changeIn(t, f, p, v, wh.ID, 1, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 2, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 3, "purchase_in")
	sku := bareSKU(v.SKUCode, "SZ")

	first := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&limit=2")
	if first.Code != http.StatusOK {
		t.Fatalf("流水页应 200，实际 %d", first.Code)
	}
	body := first.Body.String()
	if !strings.Contains(body, paginationBar) {
		t.Fatalf("流水超过一页时应出现分页条（审计 D3：这一页原先没有分页条）")
	}

	if !strings.Contains(body, "共 3 条，第 1-2 条") {
		t.Fatalf("分页信息行应给出真源总数与区间（共 3 条，第 1-2 条）")
	}
	if got := strings.Count(body, "<code>"+sku+"</code>"); got != 2 {
		t.Fatalf("第 1 页应渲染 2 条流水，实际 %d 条", got)
	}
	paginationLinkLine(t, body, 2)

	second := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&limit=2&page=2")
	if second.Code != http.StatusOK {
		t.Fatalf("流水第 2 页应 200，实际 %d", second.Code)
	}
	body = second.Body.String()
	if got := strings.Count(body, "<code>"+sku+"</code>"); got != 1 {
		t.Fatalf("第 2 页应渲染 1 条流水，实际 %d 条（翻页没换数据？）", got)
	}
	if !strings.Contains(body, "共 3 条，第 3-3 条") {
		t.Fatalf("末页的信息行应给出末页区间（共 3 条，第 3-3 条）")
	}

	single := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&limit=10")
	if strings.Contains(single.Body.String(), paginationBar) {
		t.Fatalf("单页不应渲染分页条")
	}
}

func TestInventoryMovementsDefaultPageSize(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	p := mustProduct(t, f, "缺省页大小商品")
	v := f.firstVariant(t, p.ID)
	for i := 1; i <= 51; i++ {
		changeIn(t, f, p, v, wh.ID, 1, "purchase_in")
	}
	sku := bareSKU(v.SKUCode, "SZ")

	first := httptestGet(engine, "/admin/inventory?project="+f.projectID)
	if first.Code != http.StatusOK {
		t.Fatalf("流水页应 200，实际 %d", first.Code)
	}
	body := first.Body.String()
	if got := strings.Count(body, "<code>"+sku+"</code>"); got != 50 {
		t.Fatalf("缺省每页 50 条：第 1 页应渲染 50 行，实际 %d 行", got)
	}
	if !strings.Contains(body, "共 51 条，第 1-50 条") {
		t.Fatalf("分页信息行应为「共 51 条，第 1-50 条」")
	}

	second := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&page=2")
	if got := strings.Count(second.Body.String(), "<code>"+sku+"</code>"); got != 1 {
		t.Fatalf("第 2 页应渲染 1 行（共 51 条、每页 50），实际 %d 行", got)
	}
	if !strings.Contains(second.Body.String(), "共 51 条，第 51-51 条") {
		t.Fatalf("第 2 页的信息行应为「共 51 条，第 51-51 条」")
	}
}

func TestInventoryMovementsPaginationKeepsFilters(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	p := mustProduct(t, f, "流水筛选商品")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 1, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 2, "purchase_in")
	sku := bareSKU(v.SKUCode, "SZ")

	rec := httptestGet(engine, "/admin/inventory?project="+f.projectID+
		"&limit=1&direction=in&sku="+sku+"&warehouseId="+wh.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("流水页应 200，实际 %d", rec.Code)
	}
	line := paginationLinkLine(t, rec.Body.String(), 2)
	for _, want := range []string{"direction=in", "sku=" + sku, "warehouseId=" + wh.ID} {
		if !strings.Contains(line, want) {
			t.Fatalf("翻页链接丢了筛选条件 %s：%s", want, line)
		}
	}
}

func TestInventoryMovementCountMatchesListedRows(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	p := mustProduct(t, f, "计口径一致商品")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 1, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 2, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 3, "purchase_in")
	sku := bareSKU(v.SKUCode, "SZ")
	today := time.Now().Format("2006-01-02")

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"无筛选", "", 3},
		{"按仓库", "&warehouseId=" + wh.ID, 3},
		{"按 SKU", "&sku=" + sku, 3},
		{"按方向", "&direction=in", 3},
		{"按原因", "&reasonCode=purchase_in", 3},
		{"按时间区间（今天起）", "&timeFrom=" + today, 3},
		{"时间区间落在未来应一条不剩", "&timeFrom=2100-01-01", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := "/admin/inventory?project=" + f.projectID + "&limit=2" + c.query
			got := 0

			for page := 1; page <= 20; page++ {
				rec := httptestGet(engine, base+"&page="+strconv.Itoa(page))
				if rec.Code != http.StatusOK {
					t.Fatalf("第 %d 页应 200，实际 %d", page, rec.Code)
				}
				n := strings.Count(rec.Body.String(), "move-delta-")
				got += n

				if n < 2 {
					break
				}
			}
			if got != c.want {
				t.Fatalf("逐页行数合计 %d，期望 %d（该筛选条件下真实命中数）", got, c.want)
			}

			if c.want > 2 {
				body := httptestGet(engine, base).Body.String()
				if !strings.Contains(body, fmt.Sprintf("共 %d 条", c.want)) {
					t.Fatalf("信息行的总数应与实际能翻出来的条数一致（共 %d 条）", c.want)
				}
			}
		})
	}
}

func TestInventoryPaginationI18nSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	keys := []string{
		"shell.pagination.info",
		"shell.pagination.prev",
		"shell.pagination.next",
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		var n int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE lang = ? AND item_key IN (?)",
			lang, keys).Scan(&n).Error; err != nil {
			t.Fatalf("查 %s 词条失败: %v", lang, err)
		}
		if n != int64(len(keys)) {
			t.Errorf("%s 词条 %d 条，期望 %d 条（缺的那条在页面上会显示裸 key 或回落另一种语言）",
				lang, n, len(keys))
		}
	}
}
