// Package router 组装 HTTP 路由：页面模板、静态资源、WebSocket 与 API 分组。
package router

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go-websocket/handle"
)

// R 创建 gin 引擎。viewFS / staticFS 是内嵌的页面模板与静态资源，
// h 提供 WebSocket 接入与在线列表。
func R(envMode string, viewFS, staticFS fs.FS, h *handle.Handler) (*gin.Engine, error) {
	r := gin.New()

	// Recovery 必须常开：handler 里任何一个 panic 都会直接把服务进程带崩，
	// 而 WebSocket 相关的代码里到处都是用户可控输入。
	r.Use(gin.Recovery())
	if envMode != gin.ReleaseMode {
		r.Use(gin.Logger())
	}

	// 本服务不部署在反向代理后面，显式关闭信任代理，
	// 免得 gin 启动时告警、ClientIP 也可能被 X-Forwarded-For 伪造。
	if err := r.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("设置可信代理失败: %w", err)
	}

	tpl, err := template.ParseFS(viewFS, "web/view/*")
	if err != nil {
		return nil, fmt.Errorf("解析页面模板失败: %w", err)
	}
	r.SetHTMLTemplate(tpl)

	static := http.FileServer(http.FS(staticFS))
	serveStatic := func(c *gin.Context) {
		// 静态资源随二进制一起发布，升级一次二进制内容就变一次，可以放心长缓存。
		c.Header("Cache-Control", "public, max-age=86400")
		static.ServeHTTP(c.Writer, c.Request)
	}
	r.GET("/web/static/*filepath", serveStatic)
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Request.URL.Path = "/web/static/favicon.ico"
		serveStatic(c)
	})

	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		c.String(http.StatusNotFound, "404 page not found")
	})

	viewR(r)
	apiR(r, h)

	return r, nil
}
