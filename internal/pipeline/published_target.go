package pipeline

import "fmt"

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
