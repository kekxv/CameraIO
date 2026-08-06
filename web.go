// Package web 内嵌前端构建产物（frontend/dist），
// 使 CameraIO 成为自包含单文件程序：无需在运行时提供 frontend/dist 目录。
package web

import "embed"

//go:embed frontend/dist
var Dist embed.FS
