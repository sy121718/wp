package unit

import (
	"context"
	"strings"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
)

// normalizePath 是 service 包未导出函数，本包经公开方法（Activate/RenameReserved）
// 间接验证其行为，覆盖：空、无前导斜杠、尾斜杠裁剪、根路径、非法字符、超长。

// TestPublicationNormalizePathInvalid 空路径与无前导斜杠的路径必须被拒绝
// （normalizePath 返回 ErrInvalidParam——参数格式错误，与资源占用语义区分——
// 且不产生任何副作用）。
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

// TestPublicationNormalizePathTrailingSlash 尾斜杠被裁剪；根路径 "/" 保持原样。
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
	t.Run("多尾斜杠连续裁剪", func(t *testing.T) {
		svc := newUnitService(t)
		if _, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/a/b//", PageID: pageID, ArtifactID: artifactUUID,
		}); err != nil {
			t.Fatalf("激活 /a/b// 失败: %v", err)
		}
		// 修复语义：连续裁剪尾部斜杠，同一逻辑 URL 不产生不同路由行。
		if routeMissing(t, svc, "/a/b") {
			t.Fatalf("应规范化到 /a/b")
		}
		if n := countRoutes(t, svc, "path = ?", "/a/b//"); n != 0 {
			t.Fatalf("不应残留双尾斜杠路径: %d", n)
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

// TestPublicationNormalizePathRejected normalizePath 拒绝危险/非法路径：
// 空格、URL 分隔符、穿越、引号、超长一律返回 ErrInvalidParam（参数格式错误）。
func TestPublicationNormalizePathRejected(t *testing.T) {
	// 修复语义：危险/非法路径一律拒绝（空格、URL 分隔符、穿越、引号、超长），
	// 错误语义为 ErrInvalidParam（参数格式错误），而非 ErrRouteNotFound。
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

// TestPublicationNormalizePathChineseAccepted 中文路径保持合法（业务允许非 ASCII）。
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
