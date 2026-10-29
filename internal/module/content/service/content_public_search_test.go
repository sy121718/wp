package contentservice

import (
	"context"
	"errors"
	"github.com/google/uuid"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	productcontract "go_wp/internal/module/product/contract"
	"reflect"
	"testing"
)

type publicArticleSearch struct {
	hits []*contentcontract.ArticleSearchHit
	err  error
}

func (s publicArticleSearch) SearchArticles(context.Context, string, int) ([]*contentcontract.ArticleSearchHit, error) {
	return s.hits, s.err
}

type publicProductSearch struct {
	hits        []*productcontract.ProductSearchHit
	seenProject string
}

func (s *publicProductSearch) SearchPublishedProducts(_ context.Context, project, query string, limit int) ([]*productcontract.ProductSearchHit, error) {
	s.seenProject = project
	return s.hits, nil
}

type publicSearchIndex struct {
	paths map[string]map[string]string
	calls []string
	err   error
}

func (s *publicSearchIndex) PublishedEntityIndex(_ context.Context, project, kind, lang string) (map[string]string, error) {
	s.calls = append(s.calls, project+"/"+kind+"/"+lang)
	return s.paths[kind], s.err
}

func TestPublicSearchOnlyRealPublicationFacts(t *testing.T) {
	project := uuid.NewString()
	a, b, p, h := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	products := &publicProductSearch{hits: []*productcontract.ProductSearchHit{{ID: h, Name: "没有详情"}, {ID: p, Name: "<script>原文</script>", Subtitle: "未经截断的商品副标题"}}}
	index := &publicSearchIndex{paths: map[string]map[string]string{"article": {a: "/en/real/article"}, "product": {p: "/en/real/product"}}}
	svc := NewPublicSearchService(publicArticleSearch{hits: []*contentcontract.ArticleSearchHit{{ID: b, Title: "草稿不可见"}, {ID: a, Title: "原始文章标题", Excerpt: "原始摘要"}}}, products, index)
	out, err := svc.PublicSearch(context.Background(), &contentdto.PublicSearchReq{ProjectID: project, Lang: "en-US", Query: "原文", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Articles) != 1 || out.Articles[0].ID != a || len(out.Products) != 1 || out.Products[0].ID != p || out.Products[0].Name != "<script>原文</script>" || products.seenProject != project {
		t.Fatalf("来源/公开过滤错误: %+v", out)
	}
	if !reflect.DeepEqual(index.calls, []string{project + "/article/en-US", project + "/product/en-US"}) {
		t.Fatalf("索引必须逐来源保留同一工程和语言: %+v", index.calls)
	}
	index.paths = map[string]map[string]string{}
	empty, err := svc.PublicSearch(context.Background(), &contentdto.PublicSearchReq{ProjectID: project, Query: "原文"})
	if err != nil || len(empty.Articles) != 0 || len(empty.Products) != 0 {
		t.Fatalf("空索引不能输出无URL命中: %+v %v", empty, err)
	}
	index.err = errors.New("publication unavailable")
	if _, err := svc.PublicSearch(context.Background(), &contentdto.PublicSearchReq{ProjectID: project, Query: "原文"}); err == nil {
		t.Fatal("索引失败应交由统一JSON错误出口，不能伪装无结果")
	}
}
