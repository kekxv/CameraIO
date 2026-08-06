package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

const frontendDistDir = "frontend/dist"

// registerFrontend 将前端静态文件服务注册到 Gin 引擎。
// 如果 frontend/dist 目录不存在则跳过（API-only 模式）。
func registerFrontend(r *gin.Engine) {
	if info, err := os.Stat(frontendDistDir); err != nil || !info.IsDir() {
		return
	}

	// 静态资源直接映射
	r.StaticFS("/assets", http.Dir(filepath.Join(frontendDistDir, "assets")))

	// SPA fallback：所有未匹配的路由返回 index.html
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		file := filepath.Join(frontendDistDir, path)
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			c.File(file)
			return
		}
		c.File(filepath.Join(frontendDistDir, "index.html"))
	})
}
