package pagehttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"

	"github.com/gin-gonic/gin"
)

type redirectCreateStub struct {
	pagecontract.PageService
	createErr error
	listErr   error
	created   int
	listed    string
}

func (s *redirectCreateStub) CreateRedirect(_ context.Context, _ *pagedto.RedirectCreateReq) (*pagedto.RedirectItem, error) {
	s.created++
	return nil, s.createErr
}

func (s *redirectCreateStub) ListRedirects(_ context.Context, req *pagedto.RedirectListReq) (*pagedto.RedirectListResp, error) {
	s.listed = req.ProjectID
	if s.listErr != nil {
		return nil, s.listErr
	}
	return &pagedto.RedirectListResp{
		ProjectID: "p1", Projects: []pagedto.RedirectProjectOption{{ID: "p1", Name: "官网"}},
	}, nil
}

func TestRedirectCreateFailedRequestKeepsInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name      string
		form      url.Values
		createErr error
		listErr   error
		wantCalls int
	}{
		{"业务冲突", url.Values{"project": {"p1"}, "sourcePath": {`/old"<`}, "targetPath": {"/new"}}, pageservice.ErrRedirectOccupied, nil, 1},
		{"缺少目标路径", url.Values{"project": {"p1"}, "sourcePath": {"/old"}}, nil, nil, 0},
		{"失败后列表读取异常", url.Values{"project": {"p1"}, "sourcePath": {"/old"}, "targetPath": {"/new"}}, pageservice.ErrRedirectOccupied, errors.New("list failed"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &redirectCreateStub{createErr: tc.createErr, listErr: tc.listErr}
			router := gin.New()
			router.HTMLRender = newPageErrTestRender(t)
			router.POST("/api/page/redirect/create", NewHandle(stub).RedirectCreate)
			req := httptest.NewRequest(http.MethodPost, "/api/page/redirect/create", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" {
				t.Fatalf("失败应就地显示完整页，状态=%d 跳转=%q 正文=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
			}
			out := rec.Body.String()
			for _, want := range []string{"</html>", `role="alert"`, `value="/old`, `<template id="tpl-redirect-create">`, `opener.click();`} {
				if !strings.Contains(out, want) {
					t.Errorf("失败页缺少 %q", want)
				}
			}
			if stub.listed != "p1" || stub.created != tc.wantCalls {
				t.Errorf("调用次数/工程作用域不符：list=%q create=%d", stub.listed, stub.created)
			}
			if tc.createErr != nil && !strings.Contains(out, `value="/new"`) {
				t.Error("创建业务错误后目标路径丢失")
			}
			if strings.Contains(out, `value="/old"<"`) {
				t.Error("源路径回填没有 HTML 转义")
			}
		})
	}
}

func TestRedirectCreateSuccessRendersJump(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &redirectCreateStub{}
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	router.POST("/api/page/redirect/create", NewHandle(stub).RedirectCreate)
	form := url.Values{"project": {"p1"}, "sourcePath": {"/old"}, "targetPath": {"/new"}}
	req := httptest.NewRequest(http.MethodPost, "/api/page/redirect/create?project=p1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("成功应渲染提示页（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if stub.created != 1 {
		t.Fatalf("应调用一次 CreateRedirect，实际 %d", stub.created)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="ok"`) {
		t.Fatalf(`成功提示页应有 data-jump-state="ok"：%s`, body)
	}
	// 回跳地址由服务端从表单 action 的 query 读回（project 筛选上下文），不再走 ?ok=。
	if !strings.Contains(body, "/api/page/redirect?project=p1") {
		t.Fatalf("提示页应给回本页（带 project）的链接：%s", body)
	}
}
