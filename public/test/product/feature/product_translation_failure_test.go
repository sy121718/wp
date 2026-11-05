package feature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/i18n"
)

type translationStorageFailure struct {
	*i18n.ContentWriter
	readErr, writeErr error
	writes            int
}

func (p *translationStorageFailure) LoadDetailsForProject(ctx context.Context, project, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error) {
	if p.readErr != nil {
		return nil, p.readErr
	}
	return p.ContentWriter.LoadDetailsForProject(ctx, project, lang, hashes)
}

func (p *translationStorageFailure) Upsert(ctx context.Context, items []i18n.ContentWriteItem) (int, error) {
	p.writes++
	if p.writeErr != nil {
		return 0, p.writeErr
	}
	return p.ContentWriter.Upsert(ctx, items)
}

func TestProductTranslationStorageFailureIsJSON500(t *testing.T) {
	for _, phase := range []string{"读取", "写入"} {
		t.Run(phase, func(t *testing.T) {
			f := newTRFixture(t)
			if f == nil {
				return
			}
			product, err := f.products.Create(context.Background(), &productdto.CreateReq{ProjectID: f.projectID, Name: "失败边界商品", Slug: "translation-failure"})
			if err != nil {
				t.Fatal(err)
			}
			engine := trWorkbench(t, f)
			port := &translationStorageFailure{ContentWriter: i18n.NewContentWriter(f.db)}
			failure := errors.New("SQLSTATE private_translation_store")
			wantWrites := 1
			if phase == "读取" {
				port.readErr, wantWrites = failure, 0
			} else {
				port.writeErr = failure
			}
			f.products.SetTranslationStore(port)
			rec := postForm(engine, "/api/product/translations/save", url.Values{
				"projectId": {f.projectID}, "productId": {product.ID}, "lang": {"en-US"},
				"rowContext": {"product.name"}, "rowHash": {i18n.ContentHash(product.Name)}, "rowTarget": {"Translated product"},
			})
			if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "系统内部错误") {
				t.Fatalf("存储故障必须为 HTML 500：%d %s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private_translation_store") || strings.Contains(rec.Body.String(), "SQLSTATE") || rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" {
				t.Fatalf("失败不得泄露内部错误或导航：%s", rec.Body.String())
			}
			if port.writes != wantWrites {
				t.Fatalf("读取失败不能继续写入：调用次数=%d，期望=%d", port.writes, wantWrites)
			}
			var count int64
			if err := f.db.Table("sys_translation").Where("project_id = ?", f.projectID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("故障不应留下译文：count=%d err=%v", count, err)
			}
		})
	}
}
