package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// viewR 页面路由。
func viewR(r *gin.Engine) {
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})
}
