package feature

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
	"go_wp/public/test/support"
)

func TestAttributePagingUsesUniqueTailKey(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	for i := 6; i >= 1; i-- {
		row := &productmodel.ProductAttributeEntity{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), ProjectID: f.projectID, Key: fmt.Sprintf("tie_%d", i), Name: fmt.Sprintf("同序属性%d", i), Values: []byte("[]"), Metadata: []byte("{}"), CreatedAt: at, UpdatedAt: at}
		if err := rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error { return tx.Create(row).Error }); err != nil {
			t.Fatal(err)
		}
	}
	m := productmodel.NewModel(f.db)
	for offset := 0; offset < 6; offset += 2 {
		rows, err := m.ListAttributes(ctx, f.projectID, "", nil, 2, offset)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("分页行数=%d", len(rows))
		}
		for i, row := range rows {
			want := fmt.Sprintf("00000000-0000-4000-8000-%012d", offset+i+1)
			if row.ID != want {
				t.Fatalf("同序分页未按唯一尾键排序: offset=%d got=%s want=%s", offset, row.ID, want)
			}
		}
	}
}

func TestPublishRecordListUsesUniqueTailKey(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 6; i >= 1; i-- {
		row := &productmodel.ProductEntity{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), ProjectID: f.projectID, Name: fmt.Sprintf("同序商品%d", i), Slug: fmt.Sprintf("publish-tie-%d", i), Status: "draft", Images: []byte("[]"), Metadata: []byte("{}"), CreatedAt: at, UpdatedAt: at}
		if err := rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
			return tx.Select("ID", "ProjectID", "Name", "Slug", "Status", "CreatedAt", "UpdatedAt").Create(row).Error
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := productmodel.NewModel(f.db).QueryPublishRecordPage(ctx, f.projectID, "", "", 20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 6 {
		t.Fatalf("发布记录候选行数=%d", len(page.Rows))
	}
	for i, row := range page.Rows {
		want := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
		if row.ProductID != want {
			t.Fatalf("发布记录同序未按唯一尾键排序: got=%s want=%s", row.ProductID, want)
		}
	}
}

// TestPublishRecordsPagesCompositeSortAcrossBatches 用生产迁移库验证发布记录分页的复合排序：
// 每个边界都造足一批以上的同值行，让页边界依次落到 id 分支、create_time 分支、sort 分支。
// 判据是生产 ORDER BY 本身的 ID 序列，不手抄分页逻辑。
func TestPublishRecordsPagesCompositeSortAcrossBatches(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "发布记录复合游标")

	// 三段 240 / 240 / 120 行（前两段都超过单批上限 200），边界因此落在段内：
	// 第 1 批末（行 200）是同 sort 同时间的 id 分支；第 2 批末（行 400）跳到同 sort
	// 但 create_time 更早的行，跨批覆盖 create_time 分支；第 3 批起换 sort=5，覆盖 sort 分支。
	// 时间一律用 base.Add(-i-1 分钟)，保证递减、不与同 sort 的 base 段交错。
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	type scanSeed struct {
		sort  int
		count int
		at    func(i int) time.Time
	}
	groups := []scanSeed{
		{sort: 0, count: 240, at: func(int) time.Time { return base }},
		{sort: 0, count: 240, at: func(i int) time.Time { return base.Add(-time.Duration(i+1) * time.Minute) }},
		{sort: 5, count: 120, at: func(i int) time.Time { return base.Add(-time.Duration(i+1) * time.Minute) }},
	}

	// id 用固定可排序 UUID（版本位 4、变体位 8），同组内的先后次序由构造决定。
	const zeros = "00000000-0000-4000-8000-"
	total := 0
	for _, g := range groups {
		for i := 0; i < g.count; i++ {
			total++
			id := fmt.Sprintf("%s%012d", zeros, total)
			if err := db.Exec(
				`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
				 VALUES (?, ?, ?, ?, ?, 'published', ?, ?, now())`,
				id, projectID, fmt.Sprintf("游标商品 %04d", total), fmt.Sprintf("cursor-%04d", total),
				fmt.Sprintf("SKU-CURSOR-%04d", total), g.sort, g.at(i)).Error; err != nil {
				t.Fatalf("插入商品 %d 失败：%v", total, err)
			}
		}
	}

	var want []string
	if err := db.Raw(
		`SELECT id::text FROM products WHERE project_id = ?
		 ORDER BY sort ASC, create_time DESC, id ASC`, projectID).Scan(&want).Error; err != nil {
		t.Fatalf("取生产排序序列失败：%v", err)
	}
	if len(want) != total {
		t.Fatalf("排序序列 %d 行，期望 %d 行", len(want), total)
	}

	// 整页语句读出生产排序序列：sort ASC、create_time DESC、id ASC 三段键在同值行上仍然唯一，
	// 同一批数据分页不会漏行或重复。末页由 total 决定：请求越界页会被夹到真实末页，
	// 所以循环必须停在 round(total/size)，不能靠「页号加一」试到空页 —— 那样会把末页再读一遍。
	var got []string
	const size = 200
	lastPage := (total + size - 1) / size
	for page := 1; page <= lastPage; page++ {
		res, err := productmodel.NewModel(db).QueryPublishRecordPage(context.Background(), projectID, "", "", size, page)
		if err != nil {
			t.Fatalf("整页读取失败：%v", err)
		}
		if res.Total != int64(total) {
			t.Fatalf("第 %d 页 total 应为 %d，实际 %d", page, total, res.Total)
		}
		if res.Page != page {
			t.Fatalf("第 %d 页被夹到 %d 页（total=%d size=%d）", page, res.Page, res.Total, size)
		}
		for _, row := range res.Rows {
			got = append(got, row.ProductID)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("扫描到 %d 行，期望 %d 行（有漏行或重复）", len(got), len(want))
	}
	seen := make(map[string]bool, len(got))
	for _, id := range got {
		if seen[id] {
			t.Fatalf("批次之间出现重复商品：%s", id)
		}
		seen[id] = true
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 行与生产排序不一致：got=%s want=%s", i, got[i], want[i])
		}
	}
}
