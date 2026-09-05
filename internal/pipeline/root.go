package pipeline

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultArtifactRoot 返回产物根目录：GO_WP_ARTIFACT_ROOT 可覆盖，
// 否则默认 public/runtime/artifacts。
//
// 全系统唯一来源：控制面 LocalStore（page service 装配）与访问面
// ActiveRoot（routes.go setupStaticFace）都必须经此取值，禁止在
// 其他位置重复读取环境变量或硬编码默认路径（审计 Medium：ActiveRoot 双源）。
func DefaultArtifactRoot() string {
	root := strings.TrimSpace(os.Getenv("GO_WP_ARTIFACT_ROOT"))
	if root == "" {
		root = filepath.Join("public", "runtime", "artifacts")
	}
	return root
}

// ActiveRoot 返回静态访问面激活目录：产物根下两级 {root}/public/active。
// 由访问面（/site StaticFS）与控制面（LocalPublicationStore）共同使用。
func ActiveRoot() string {
	return filepath.Join(DefaultArtifactRoot(), "public", "active")
}
