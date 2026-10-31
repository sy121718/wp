package pubdto_test

import (
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
)

func TestSeoAuditReqResolveProject(t *testing.T) {
	cases := []struct {
		name  string
		body  *pubdto.SeoAuditReq
		query *pubdto.SeoAuditReq
		want  string
	}{
		{name: "body 优先于 query", body: &pubdto.SeoAuditReq{Project: "body-p"}, query: &pubdto.SeoAuditReq{Project: "query-p"}, want: "body-p"},
		{name: "body 去空白", body: &pubdto.SeoAuditReq{Project: "  body-p\t"}, want: "body-p"},
		{name: "body 空白回落 query", body: &pubdto.SeoAuditReq{Project: "   "}, query: &pubdto.SeoAuditReq{Project: "query-p"}, want: "query-p"},
		{name: "body 空串回落 query", body: &pubdto.SeoAuditReq{}, query: &pubdto.SeoAuditReq{Project: " query-p "}, want: "query-p"},
		{name: "只有 body", body: &pubdto.SeoAuditReq{Project: "only-body"}, want: "only-body"},
		{name: "只有 query", query: &pubdto.SeoAuditReq{Project: "only-query"}, want: "only-query"},
		{name: "双空", body: &pubdto.SeoAuditReq{}, query: &pubdto.SeoAuditReq{}, want: ""},
		{name: "双空白", body: &pubdto.SeoAuditReq{Project: " "}, query: &pubdto.SeoAuditReq{Project: "\n"}, want: ""},
		{name: "body 为 nil 回落 query", query: &pubdto.SeoAuditReq{Project: "q"}, want: "q"},
		{name: "query 为 nil 用 body", body: &pubdto.SeoAuditReq{Project: "b"}, want: "b"},
		{name: "双 nil", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.body.ResolveProject(tc.query); got != tc.want {
				t.Fatalf("ResolveProject = %q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestSeoAuditReqResolveProjectDoesNotMutate(t *testing.T) {
	body := &pubdto.SeoAuditReq{Project: "  raw  "}
	query := &pubdto.SeoAuditReq{Project: " q "}
	if got := body.ResolveProject(query); got != "raw" {
		t.Fatalf("ResolveProject = %q", got)
	}
	if body.Project != "  raw  " || query.Project != " q " {
		t.Fatalf("入参被改写：body=%q query=%q", body.Project, query.Project)
	}
}
