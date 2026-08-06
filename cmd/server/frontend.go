package main

import (
	"io/fs"
	"log"
	"net/http"
	"strings"

	// 根目录 web 包内嵌 frontend/dist，无需外部目录
	web "CameraIO"

	"github.com/gin-gonic/gin"
)

// registerFrontend 将内嵌的前端构建产物注册到 Gin 引擎。
// 前端通过 //go:embed 打进二进制，任何工作目录下均可访问。
func registerFrontend(r *gin.Engine) {
	dist, err := fs.Sub(web.Dist, "frontend/dist")
	if err != nil {
		log.Printf("[frontend] 内嵌前端不可用: %v", err)
		return
	}
	// 预读 index.html：http.FileServer 会对 "/index.html" 请求 301 重定向到根路径，
	// 所以 SPA fallback 直接返回内容而非交给 FileServer。
	indexHTML, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		log.Printf("[frontend] 内嵌 index.html 不可用: %v", err)
		return
	}

	// 静态资源直接映射（JS/CSS/图片等）
	r.StaticFS("/assets", http.FS(dist))

	// SPA fallback：所有未匹配的路由返回 index.html
	r.NoRoute(func(c *gin.Context) {
		p := strings.TrimPrefix(c.Request.URL.Path, "/")
		if p != "" && p != "index.html" {
			if info, err := fs.Stat(dist, p); err == nil && !info.IsDir() {
				c.FileFromFS(c.Request.URL.Path, http.FS(dist))
				return
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
}
