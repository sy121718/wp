package unit

import (
	"context"
	"strings"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
)

func TestPublicationNormalizePathInvalid(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"空字符串", ""},
		{"纯空格", "   "},
		{"无前导斜杠", "about"},
		{"无前导斜杠带尾斜杠", "about/"},
		{"前导空格后接斜杠", " /about"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newUnitService(t)
			_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
				ProjectID: projectID, Path: tc.path, PageID: pageID, ArtifactID: artifactUUID,
			})
			containsErr(t, err, pubenums.ErrInvalidParam)
			if n := countReceipts(t, svc, "1 = 1"); n != 0 {
				t.Fatalf("非法路径不应写入回执: %d", n)
			}
			if n := countRoutes(t, svc, "1 = 1"); n != 0 {
				t.Fatalf("非法路径不应写入路由: %d", n)
			}
		})
	}
}

func TestPublicationNormalizePathTrailingSlash(t *testing.T) {
	t.Run("尾斜杠裁剪", func(t *testing.T) {
		svc := newUnitService(t)
		_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/about/", PageID: pageID, ArtifactID: artifactUUID,
		})
		if err != nil {
			t.Fatalf("激活 /about/ 失败: %v", err)
		}
		if routeMissing(t, svc, "/about") {
			t.Fatal("规范化后应落在 /about")
		}
		if n := countRoutes(t, svc, "path = ?", "/about/"); n != 0 {
			t.Fatalf("不应残留尾斜杠路径: %d", n)
		}
	})
	t.Run("重复分隔符被拒绝", func(t *testing.T) {
		svc := newUnitService(t)

		if _, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/a/b//", PageID: pageID, ArtifactID: artifactUUID,
		}); err == nil {
			t.Fatal("含重复分隔符的路径应被拒绝，实际成功")
		}
		if !routeMissing(t, svc, "/a/b") && !routeMissing(t, svc, "/a/b//") {
			t.Fatal("被拒绝的路径不应留下任何路由行")
		}
	})
	t.Run("根路径保持", func(t *testing.T) {
		svc := newUnitService(t)
		if _, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/", PageID: pageID, ArtifactID: artifactUUID,
		}); err != nil {
			t.Fatalf("激活根路径失败: %v", err)
		}
		if routeMissing(t, svc, "/") {
			t.Fatal("根路径应保持为 /")
		}
	})
}

func TestPublicationNormalizePathRejected(t *testing.T) {

	cases := []struct {
		name string
		path string
	}{
		{"含内部空格", "/a b"},
		{"含点号路径穿越", "/../etc/passwd"},
		{"含查询字符", "/a?b=1"},
		{"含井号", "/a#b"},
		{"含引号", `/a"b`},
		{"超长路径", "/" + strings.Repeat("x", 2048)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newUnitService(t)
			_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
				ProjectID: projectID, Path: tc.path, PageID: pageID, ArtifactID: artifactUUID,
			})
			containsErr(t, err, pubenums.ErrInvalidParam)
		})
	}
}

func TestPublicationNormalizePathChineseAccepted(t *testing.T) {
	svc := newUnitService(t)
	route, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/关于我们", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err != nil {
		t.Fatalf("中文路径应被接受: %v", err)
	}
	if route.Path != "/关于我们" {
		t.Fatalf("中文路径不应被改写: got %q", route.Path)
	}
}
