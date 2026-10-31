package pipeline

import (
	"os"
	"path/filepath"
	"strings"
)

func DefaultArtifactRoot() string {
	root := strings.TrimSpace(os.Getenv("GO_WP_ARTIFACT_ROOT"))
	if root == "" {
		root = filepath.Join("public", "runtime", "artifacts")
	}
	return root
}

func ActiveRoot() string {
	return filepath.Join(DefaultArtifactRoot(), "public", "active")
}
