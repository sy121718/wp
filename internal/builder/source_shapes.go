package builder

// AssetVariant 是资产探测的中立窄形状：只保留展示需要的 URL 与宽度。
type AssetVariant struct {
	URL   string
	Width int
}

// MediaVariantRef 是本工具对媒体变体引用的输入形状。
type MediaVariantRef = AssetVariant
