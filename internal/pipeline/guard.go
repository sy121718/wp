package pipeline

import (
	fmt "fmt"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/publish"
	"strings"

	"go_wp/internal/builder"
)

const (
	GuardPageFileName = publish.GuardPageFileName

	GuardMetaFileName = publish.GuardMetaFileName
)

const guardMetaVersion = 1

const GuardBackPlaceholder = publish.GuardBackPlaceholder

type GuardMeta = publish.GuardMeta

type ManifestAccess = publish.ManifestAccess

func GuardFingerprint(passwordHash string) string {
	return publish.GuardFingerprint(passwordHash)
}

func ReadGuardMeta(dir string) (*GuardMeta, error) {
	return publish.ReadGuardMeta(dir)
}

func GuardPageBody(dir string) (body []byte, ok bool) {
	return publish.GuardPageBody(dir)
}

func GuardPageWithPath(body []byte, path string) []byte {
	return publish.GuardPageWithPath(body, path)
}

func BuildGuardEntries(access *builder.AccessGuardSettings, lang string) (files map[string][]byte, meta *ManifestAccess) {
	if access.IsPublic() {
		return nil, nil
	}
	kind := access.TypeOrDefault()
	gm := GuardMeta{
		Version: guardMetaVersion,
		Type:    kind,
		Lang:    lang,
	}
	if kind == builder.AccessPassword {
		gm.PasswordHash = strings.TrimSpace(access.PasswordHash)
		gm.Fingerprint = GuardFingerprint(gm.PasswordHash)
	}

	metaJSON, err := json.Marshal(gm)
	if err != nil {

		return nil, nil
	}
	return map[string][]byte{
		GuardPageFileName: publish.RenderGuardPage(gm, GuardBackPlaceholder, false),
		GuardMetaFileName: metaJSON,
	}, &ManifestAccess{Type: kind}
}

func PageGuardEntries(docJSON []byte, lang string) (map[string][]byte, *ManifestAccess, error) {
	page, err := builder.ParsePage(docJSON)
	if err != nil {
		return nil, nil, err
	}
	if page == nil {
		return nil, nil, fmt.Errorf("页面文档为空")
	}
	files, meta := BuildGuardEntries(page.Settings.Access, lang)
	return files, meta, nil
}
