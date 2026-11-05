package pipeline

import (
	"go_wp/internal/builder"
	"go_wp/pkg/publish"
)

var ActivePublishedPath = publish.ActivePublishedPath
var ActiveRoot = publish.ActiveRoot

type Artifact = publish.Artifact

var ArtifactLocator = publish.ArtifactLocator
var AssetContentType = publish.AssetContentType
var BlockKey = publish.BlockKey
var CleanSiteRel = publish.CleanSiteRel
var ContentCollectionKey = publish.ContentCollectionKey
var ContentTemplateKey = publish.ContentTemplateKey
var DefaultArtifactRoot = publish.DefaultArtifactRoot
var DefaultPathLocker = publish.DefaultPathLocker

type DepKey = publish.DepKey

const DepKindContentCollection = publish.DepKindContentCollection
const DepKindContentTemplate = publish.DepKindContentTemplate
const DepKindMenu = publish.DepKindMenu
const DepKindNavigation = publish.DepKindNavigation

type Dependency = publish.Dependency
type RedirectArtifact = publish.RedirectArtifact
type DependencyTarget = publish.DependencyTarget

var DirectContentKey = publish.DirectContentKey

type Fanout = publish.Fanout

var I18NContentDependency = publish.I18NContentDependency
var I18NDependency = publish.I18NDependency
var IsPrecompressible = publish.IsPrecompressible

type LangURLRule = publish.LangURLRule
type LocalPublicationStore = publish.LocalPublicationStore
type LocalStore = publish.LocalStore
type Locator = publish.Locator

var LogContentTranslationMisses = publish.LogContentTranslationMisses

type Manifest = publish.Manifest

const ManifestSchemaVersion = publish.ManifestSchemaVersion

var MenuKey = publish.MenuKey
var NavigationKey = publish.NavigationKey
var NewArtifact = publish.NewArtifact
var NewFanout = publish.NewFanout
var NewLangURLRule = publish.NewLangURLRule
var NewRedirectArtifact = publish.NewRedirectArtifact
var NormalizeURL = publish.NormalizeURL
var ParseRedirectEntry = publish.ParseRedirectEntry

const PublicationNone = publish.PublicationNone
const PublicationPage = publish.PublicationPage

type PublicationPathLocker = publish.PublicationPathLocker
type PublicationPlan = publish.PublicationPlan

const PublicationRedirect = publish.PublicationRedirect

var ReadNotFoundPage = publish.ReadNotFoundPage

var RenderGuardPage = publish.RenderGuardPage
var ResolveActiveEntry = publish.ResolveActiveEntry
var SHA256 = publish.SHA256

const SourceTypePage = publish.SourceTypePage
const SourceTypePresentation = publish.SourceTypePresentation

type StaleRebuilder = publish.StaleRebuilder

var SyncNotFoundPage = publish.SyncNotFoundPage
var WithPathLock = publish.WithPathLock

const PrecompressedSuffix = publish.PrecompressedSuffix

var ErrPathLockerUnavailable = publish.ErrPathLockerUnavailable

// PublishDiagnostics 把构建器降级诊断转成发布内核的中立事实。
func PublishDiagnostics(items []builder.Degrade) []publish.Diagnostic {
	return diagnosticsOf(items)
}

type PublicationState = publish.PublicationState
type ActiveLinkIssue = publish.ActiveLinkIssue
type ManifestTranslationMisses = publish.ManifestTranslationMisses
type FrozenBlockAlias = publish.FrozenBlock
type RedirectDirective = publish.RedirectDirective

const PrecompressMinSize = publish.PrecompressMinSize
const TranslationPolicyFallback = publish.TranslationPolicyFallback
const DepKindPageTarget = publish.DepKindPageTarget
const DepKindPresentationTarget = publish.DepKindPresentationTarget
const DepKindBlock = publish.DepKindBlock
const DepKindRuntime = publish.DepKindRuntime

var EncodeManifest = publish.EncodeManifest

var NewArtifactWithEntries = publish.NewArtifactWithEntries

// LocatorHash 取产物的定位键（测试与审计共用）。
func LocatorHash(loc Locator) string { return publish.LocatorHash(loc) }

var NotFoundPagePath = publish.NotFoundPagePath

const NotFoundFileName = publish.NotFoundFileName

var IsSiteRootFile = publish.IsSiteRootFile

var GzipDeterministic = publish.GzipDeterministic

var PrecompressEntries = publish.PrecompressEntries

var ManifestDirForLang = publish.ManifestDirForLang

const gzipOSUnknown = publish.GzipOSUnknown

type ProcessPathLocker = publish.ProcessPathLocker

var NewProcessPathLocker = publish.NewProcessPathLocker

const DepKindDirectContent = publish.DepKindDirectContent
const DepKindMedia = publish.DepKindMedia
const DepKindSiteSetting = publish.DepKindSiteSetting
const DepKindI18N = publish.DepKindI18N

var LangPath = publish.LangPath
var StripLangPath = publish.StripLangPath

const I18NDependencyKey = publish.I18NDependencyKey
const I18NContentDependencyKey = publish.I18NContentDependencyKey

var NormalizeLang = publish.NormalizeLang

const DependencyKindI18N = publish.DependencyKindI18N
