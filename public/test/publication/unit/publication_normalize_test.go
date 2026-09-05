package unit

import (
	"context"
	"strings"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

// normalizePath 是 service 包未导出函数，本包经公开方法（Activate/RenameReserved）
// 间接验证其行为，覆盖：空、无前导斜杠、尾斜杠裁剪、根路径、非法字符、超长。

// TestPublicationNormalizePathInvalid 空路径与无前导斜杠的路径必须被拒绝
// （normalizePath 返回 ErrRouteNotFound 且不产生任何副作用）。
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
			containsErr(t, err, pubenums.ErrRouteNotFound)
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
	t.Run("多尾斜杠仅裁剪一个", func(t *testing.T) {
		svc := newUnitService(t)
		if _, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/a/b//", PageID: pageID, ArtifactID: artifactUUID,
		}); err != nil {
			t.Fatalf("激活 /a/b// 失败: %v", err)
		}
		// strings.TrimSuffix 只移除一个尾部斜杠：/a/b// → /a/b/
		// （多尾斜杠未完全规范化，同一逻辑 URL 可能产生不同路由行，见报告）。
		if routeMissing(t, svc, "/a/b/") {
			t.Fatalf("当前实现只裁剪一个尾斜杠，应落在 /a/b/")
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

// TestPublicationNormalizePathAccepted 当前 normalizePath 不校验非法字符与长度，
// 含空格/中文/保留字符/超长路径均被接受。此测试固化当前行为，
// 缺陷判定见报告（缺少路径字符与长度校验）。
func TestPublicationNormalizePathAccepted(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"含内部空格", "/a b"},
		{"含中文", "/关于我们"},
		{"含点号路径穿越", "/../etc/passwd"},
		{"含查询字符", "/a?b=1"},
		{"含井号", "/a#b"},
		{"含引号", `/a"b`},
		{"超长路径", "/" + strings.Repeat("x", 2048)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newUnitService(t)
			route, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
				ProjectID: projectID, Path: tc.path, PageID: pageID, ArtifactID: artifactUUID,
			})
			if err != nil {
				t.Fatalf("路径 %q 应被当前实现接受: %v", tc.path, err)
			}
			if route.Path != tc.path {
				t.Fatalf("路径不应被改写: got %q want %q", route.Path, tc.path)
			}
			if route.RouteKind != pubmodel.RouteActive {
				t.Fatalf("应为 active: %+v", route)
			}
		})
	}
}
