package aihttp

// ai_fab_image.go — 悬浮球提问里附带的图片。
//
// 只接受 **data URI**，不接受 http(s) 地址。理由不是省事，是安全：
// 一个用户可控的 URL 交给「服务端替你去取」的链路，等于把内网地址与云元数据
// （169.254.169.254）也变成可读对象 —— 而这条链路（浏览器 → 本服务 → 上游模型）
// 根本不需要本服务去**取**图：前端读文件转 base64 就够了，图从来不必
// 经过我们这一跳。收下 URL 只会凭空多出一个 SSRF 面。
//
// 校验的粒度按「能不能安全地转发给上游」定：前缀、张数、单张体积。
// 内容本身不解析（我们不是图片处理器），上游的模型会看到它是不是图。

import (
	"encoding/json"
	"strings"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
)

const (
	// fabImageMaxCount 一次提问最多带几张图。
	//
	// 上限存在的原因是上下文：每张图都会以 base64 形态进请求（体积约为原图的 4/3），
	// 张数不设限时一条提问可以轻易把输入推到几十万 token，而失败形态是
	// 「上游报上下文超限」，用户完全看不出是自己粘了太多图。
	fabImageMaxCount = 4
	// fabImageMaxBytes 单张图的 data URI 长度上限（约等于 3MB 原图）。
	//
	// 按**编码后**长度算：真正占上下文与内存的是 base64 那一串。
	fabImageMaxBytes = 4 << 20
	// fabImageDataPrefix 合法的 data URI 前缀。
	fabImageDataPrefix = "data:image/"
)

// fabImageError 图片校验失败的原因（取值为 aienums 的 MsgFab* 常量）。
type fabImageError struct {
	Key string
}

// parseFabImages 解析前端随提问带上来的图片。
//
// 两个入参都是 JSON 数组文本（form 里传数组只能是 JSON 字符串）：
// images 是 data URI 数组，labels 是与之一图对一图的标识（文件名）。
// 返回的 aidto.SendMessageReq 片段里，Images 给模型、ImageLabels 给人看。
//
// 校验失败时**整批拒绝**（返回错误）而不是丢掉坏的那几张：悄悄少发一张图的
// 症状是「模型说看不到第二张」，用户不会想到是服务端替他做了取舍。
func parseFabImages(rawImages, rawLabels string) ([]string, []string, *fabImageError) {
	rawImages = strings.TrimSpace(rawImages)
	if rawImages == "" {
		return nil, nil, nil
	}
	var images []string
	if err := json.Unmarshal([]byte(rawImages), &images); err != nil {
		return nil, nil, &fabImageError{Key: aienums.MsgFabImageBadFormat}
	}
	// 空数组与没有图是同一件事，不必让调用方再判一次。
	images = nonEmptyStrings(images)
	if len(images) == 0 {
		return nil, nil, nil
	}
	if len(images) > fabImageMaxCount {
		return nil, nil, &fabImageError{Key: aienums.MsgFabImageTooMany}
	}
	for _, img := range images {
		if !strings.HasPrefix(img, fabImageDataPrefix) {
			return nil, nil, &fabImageError{Key: aienums.MsgFabImageBadFormat}
		}
		if len(img) > fabImageMaxBytes {
			return nil, nil, &fabImageError{Key: aienums.MsgFabImageTooLarge}
		}
	}

	var labels []string
	if strings.TrimSpace(rawLabels) != "" {
		// 标识解不出来不算错误：它只影响界面上那行小字，图本身照发。
		_ = json.Unmarshal([]byte(rawLabels), &labels)
		labels = nonEmptyStrings(labels)
	}
	// 标识与图**一图对一图**：数量对不上时补齐（截断多余的 / 用占位补缺的），
	// 否则界面会把 A 图的名字画在 B 图下面 —— 那种错位比没有名字更难发现。
	labels = alignLabels(labels, len(images))
	return images, labels, nil
}

// nonEmptyStrings 去掉空白项（前端可能因为一次粘贴里的换行产生空串）。
func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// alignLabels 把标识数组对齐到 n 个（多余的截掉，缺少的补占位）。
func alignLabels(labels []string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		if i < len(labels) {
			out[i] = labels[i]
			continue
		}
		out[i] = fabImageFallbackLabel
	}
	return out
}

// fabImageFallbackLabel 没有文件名时界面上的占位标识。
//
// 用固定的中文占位而不是空串：空串会让界面画出「📎 图片：」这样一句没头没尾的话，
// 看起来像渲染坏了。
const fabImageFallbackLabel = "图片"

// fabImageRequest 给 SendMessageReq 填上图片相关的三个字段（images / labels）。
func fabImageRequest(req *aidto.SendMessageReq, images, labels []string) {
	req.Images = images
	req.ImageLabels = labels
}
