package plugindto

import "mime/multipart"

// InstallFormReq 是插件安装的 multipart 表单形状。
//
// 文件字段必须是 *multipart.FileHeader：Gin 的 multipart 映射只对
// *multipart.FileHeader / multipart.FileHeader / 其切片接受文件键，
// 声明成 string 会直接返回 ErrMultiFileHeader。大小上限、空文件与 zip 解析
// 仍在控制器与 service 完成。
type InstallFormReq struct {
	File *multipart.FileHeader `form:"file"`
}
