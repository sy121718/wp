package pipeline

import "fmt"

// ActivePublishedPath 校验账本路径对应的真实访问面产物，不把缺文件或旧重定向当页面。
// 未激活返回空路径；账本与访问面不一致、文件损坏则明确失败，由发布链保留原产物。
func ActivePublishedPath(publication PublicationStore, artifacts Store, path, hash string) (string, error) {
	if path == "" || hash == "" {
		return "", nil
	}
	if publication == nil || artifacts == nil {
		return "", fmt.Errorf("发布目标存储未装配")
	}
	state, err := publication.Inspect(path)
	if err != nil {
		return "", err
	}
	if state == nil || state.Kind == PublicationNone {
		return "", nil
	}
	if state.Kind != PublicationPage || state.Locator == nil {
		return "", fmt.Errorf("发布目标不是当前激活的页面: %s", path)
	}
	if err := artifacts.VerifyArtifact(*state.Locator, hash); err != nil {
		return "", err
	}
	return path, nil
}
