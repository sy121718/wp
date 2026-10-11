package siteurl

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
)

type projectDetailStub struct {
	response *projectcontract.ProjectResp
	err      error
	ctx      context.Context
	id       string
	calls    int
}

var _ ProjectDetailReader = (*projectDetailStub)(nil)
var _ ProjectDetailReader = (projectcontract.ProjectService)(nil)

func (s *projectDetailStub) Detail(ctx context.Context, req *projectcontract.DetailReq) (*projectcontract.ProjectResp, error) {
	s.ctx, s.id = ctx, req.ID
	s.calls++
	return s.response, s.err
}

func TestURLPatternsOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		noReader  bool
		projectID string
		missing   bool
		settings  string
		err       error
		want      map[string]string
		wantCalls int
	}{
		{name: "nil reader", noReader: true, projectID: "project-1"},
		{name: "empty project ID"},
		{name: "project failure", projectID: "project-1", err: errors.New("project failed"), settings: `{"urlPatterns":{"article":"/news/{slug}"}}`, wantCalls: 1},
		{name: "missing project", projectID: "project-1", missing: true, wantCalls: 1},
		{name: "empty settings", projectID: "project-1", wantCalls: 1},
		{name: "missing patterns", projectID: "project-1", settings: `{}`, wantCalls: 1},
		{name: "null settings", projectID: "project-1", settings: `null`, wantCalls: 1},
		{name: "non object settings", projectID: "project-1", settings: `[]`, wantCalls: 1},
		{name: "malformed settings", projectID: "project-1", settings: `{`, wantCalls: 1},
		{name: "null patterns", projectID: "project-1", settings: `{"urlPatterns":null}`, wantCalls: 1},
		{name: "empty patterns", projectID: "project-1", settings: `{"urlPatterns":{}}`, want: map[string]string{}, wantCalls: 1},
		{name: "custom patterns", projectID: "project-1", settings: `{"urlPatterns":{"article":"/news/{slug}","product":"/shop/{id}"}}`, want: map[string]string{KindArticle: "/news/{slug}", KindProduct: "/shop/{id}"}, wantCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub := &projectDetailStub{err: tc.err}
			if !tc.missing {
				stub.response = &projectcontract.ProjectResp{ID: "project-1", Settings: json.RawMessage(tc.settings)}
			}
			var reader ProjectDetailReader = stub
			if tc.noReader {
				reader = nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if got := URLPatternsOf(ctx, reader, tc.projectID); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("patterns = %#v, want %#v", got, tc.want)
			}
			if stub.calls != tc.wantCalls {
				t.Fatalf("Detail calls = %d, want %d", stub.calls, tc.wantCalls)
			}
			if tc.wantCalls > 0 && (stub.ctx != ctx || stub.id != tc.projectID) {
				t.Fatalf("Detail did not receive original context and project ID: %v, %q", stub.ctx, stub.id)
			}
		})
	}
}

func TestDetailPathForProject(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		reader ProjectDetailReader
		kind   string
		slug   string
		id     string
		want   string
	}{
		{"nil reader article default", nil, KindArticle, "hello", "a1", "/blog/hello"},
		{"project failure product default", &projectDetailStub{err: errors.New("project failed")}, KindProduct, "tee", "p1", "/products/tee"},
		{"missing project slug fallback", &projectDetailStub{}, KindProduct, "", "p1", "/products/p1"},
		{"empty settings article default", &projectDetailStub{response: &projectcontract.ProjectResp{}}, KindArticle, "hello", "a1", "/blog/hello"},
		{"custom product pattern", &projectDetailStub{response: &projectcontract.ProjectResp{Settings: json.RawMessage(`{"urlPatterns":{"product":"/shop/{id}-{slug}/"}}`)}}, KindProduct, " tee ", " p1 ", "/shop/p1-tee"},
		{"blank custom pattern default", &projectDetailStub{response: &projectcontract.ProjectResp{Settings: json.RawMessage(`{"urlPatterns":{"article":" "}}`)}}, KindArticle, "hello", "a1", "/blog/hello"},
		{"unknown type", nil, "unknown", "hello", "a1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DetailPathForProject(t.Context(), tc.reader, "project-1", tc.kind, tc.slug, tc.id); got != tc.want {
				t.Fatalf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestURLPatternsOfPropagatesCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stub := &projectDetailStub{err: context.Canceled}
	if got := URLPatternsOf(ctx, stub, "project-1"); got != nil {
		t.Fatalf("canceled project read = %#v, want nil", got)
	}
	if stub.calls != 1 || stub.ctx != ctx || stub.ctx.Err() != context.Canceled {
		t.Fatal("Detail did not receive the original canceled context")
	}
}
