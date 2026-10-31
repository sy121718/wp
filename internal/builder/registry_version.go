package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	"go_wp/internal/builder/core"
)

var (
	buildFingerprintOnce  sync.Once
	buildFingerprintValue string
)

func RegistryVersion() string {
	types := core.Types()
	if len(types) == 0 {

		return ""
	}
	h := sha256.New()
	_, _ = h.Write([]byte(buildFingerprint()))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(componentManifestFingerprint(types)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func buildFingerprint() string {
	buildFingerprintOnce.Do(func() {
		buildFingerprintValue = computeBuildFingerprint()
	})
	return buildFingerprintValue
}

func computeBuildFingerprint() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, mod := "", ""
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			mod = s.Value
		}
	}
	if rev == "" {

		rev = "novcs"
	}
	return rev + "|" + mod
}

func componentManifestFingerprint(types []string) string {
	h := sha256.New()
	for _, t := range types {
		_, _ = h.Write([]byte(t))
		_, _ = h.Write([]byte{'|'})
		comp, err := core.Lookup(t)
		if err != nil {
			continue
		}
		if sp, ok := comp.(core.SpecProvider); ok {
			if spec := sp.PropsSpec(); spec != nil {
				_, _ = h.Write([]byte(propsSchemaFingerprint(reflect.TypeOf(spec), map[reflect.Type]bool{})))
			}
		}
		_, _ = h.Write([]byte{'|'})
		if tp, ok := comp.(core.TranslatableProvider); ok {
			fields := append([]string(nil), tp.Translatable()...)
			sort.Strings(fields)
			_, _ = h.Write([]byte(strings.Join(fields, ",")))
		}
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func propsSchemaFingerprint(t reflect.Type, seen map[reflect.Type]bool) string {
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return ""
	}
	seen[t] = true
	defer delete(seen, t)
	var b strings.Builder
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		b.WriteString(f.Name)
		b.WriteByte(':')
		b.WriteString(f.Tag.Get("json"))
		b.WriteByte(':')
		b.WriteString(f.Tag.Get("ct"))
		b.WriteByte(';')
		if sub := propsSchemaFingerprint(f.Type, seen); sub != "" {
			b.WriteString(sub)
			b.WriteByte(';')
		}
	}
	return b.String()
}
