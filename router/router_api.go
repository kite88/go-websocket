package router

import (
	"github.com/gin-gonic/gin"

	"go-websocket/handle"
)

// apiR 装配 WebSocket 入口与 API 路由。
func apiR(r *gin.Engine, h *handle.Handler) {
	// WebSocket 刻意不放进 /api 分组：它是长连接，与「一问一答」的接口放在
	// 一起会让日志、鉴权这类分组中间件失去意义；路径也短（/ws?uid=xxx）。
	r.GET("/ws", h.WebSocket)

	api := r.Group("/api")
	{
		api.GET("/online", h.Online)
	}
}
