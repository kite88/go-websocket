// Package handle 实现 WebSocket 接入、消息投递，以及一个在线用户查询接口。
//
// 并发安全全部收敛在两处：Hub 的 clients 映射只在单个 goroutine 里改动，
// 连接上的写操作只在 writePump 里发生（见 hub.go 的说明）。
package handle

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"go-websocket/common"
)

// Config 是 WebSocket 的可调参数。
//
// 由 main 从配置文件读出后显式注入，而不是在包里读全局配置：接口可测性来自
// 「依赖可见」——测试可以给出一份很激进的心跳参数，而不用去改配置文件。
type Config struct {
	ReadBufferSize  int
	WriteBufferSize int
	MaxMessageSize  int64
	PingPeriod      time.Duration
	PongWait        time.Duration
	WriteWait       time.Duration
	SendQueue       int
	AllowAllOrigins bool
}

// DefaultConfig 返回一组保守的默认值，配置缺项时由 normalize 兜底。
func DefaultConfig() Config {
	return Config{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		MaxMessageSize:  4096,
		PingPeriod:      30 * time.Second,
		PongWait:        60 * time.Second,
		WriteWait:       10 * time.Second,
		SendQueue:       64,
	}
}

// normalize 把非法（<=0）的容量参数换回默认值，避免 make(chan, 0) 这种
// 「看似能用、实际把每次投递都变成阻塞」的坑。
func (c Config) normalize() Config {
	def := DefaultConfig()
	if c.ReadBufferSize <= 0 {
		c.ReadBufferSize = def.ReadBufferSize
	}
	if c.WriteBufferSize <= 0 {
		c.WriteBufferSize = def.WriteBufferSize
	}
	if c.SendQueue <= 0 {
		c.SendQueue = def.SendQueue
	}
	return c
}

// Handler 持有连接中心与配置，是 HTTP 层唯一的入口。
type Handler struct {
	hub *Hub
	cfg Config
}

// New 创建 Handler 并启动连接中心。
func New(cfg Config) *Handler {
	return &Handler{hub: NewHub(), cfg: cfg.normalize()}
}

// Close 关闭连接中心并断开全部客户端。
func (h *Handler) Close() { h.hub.Close() }

// WebSocket 把 HTTP 请求升级成 WebSocket 连接。GET /ws?uid=xxx
func (h *Handler) WebSocket(ctx *gin.Context) {
	uid := common.TrimSpace(ctx.Query("uid"))
	if !common.ValidUID(uid) {
		// 校验必须在升级之前，并以 JSON 返回：升级之后就只能靠关闭帧表达错误，
		// 前端拿不到原因，只能干瞪眼。
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "uid 非法：只允许字母、数字、下划线、连字符，长度 1~64"})
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  h.cfg.ReadBufferSize,
		WriteBufferSize: h.cfg.WriteBufferSize,
		CheckOrigin:     h.checkOrigin,
	}
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		// Upgrade 内部已经写过响应（400 / 403 等），这里再写会变成 superfluous
		// WriteHeader，只记日志。
		log.Printf("升级 WebSocket 失败(uid=%s): %v", uid, err)
		return
	}

	c := &Client{
		uid:  uid,
		conn: conn,
		send: make(chan []byte, h.cfg.SendQueue),
		done: make(chan struct{}),
	}
	select {
	case h.hub.register <- c:
	case <-h.hub.done:
		// 服务正在停机，别再建立新连接。
		_ = conn.Close()
		return
	}

	// 两个泵各自独立退出：读循环由对端关闭 / 读超时驱动，写循环由 done 驱动。
	go h.writePump(c)
	go h.readPump(c)
}

// Online 返回当前在线用户。GET /api/online
func (h *Handler) Online(ctx *gin.Context) {
	uids := h.hub.Online()
	if uids == nil {
		uids = []string{}
	}
	ctx.JSON(http.StatusOK, gin.H{"count": len(uids), "users": uids})
}

// checkOrigin 决定是否接受跨站连接。
//
// 默认只允许同源：页面由本服务自己托管，同源天然成立。allow_all_origins 开关
// 留给「用本地文件直接打开页面」或第三方调试工具的场景。不带 Origin 头的
// 客户端（wscat、脚本压测）一律放行——那不属于浏览器的跨站场景
// （浏览器发起的 WebSocket 握手必然带 Origin）。
func (h *Handler) checkOrigin(r *http.Request) bool {
	if h.cfg.AllowAllOrigins {
		return true
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// readPump 读循环：唯一的读方，读到消息就交给 Hub 投递。
func (h *Handler) readPump(c *Client) {
	defer func() {
		// 先注销再关连接：Hub 一旦把该连接移出映射，就不会再有人往它的
		// send 里投递，随后的关闭过程是干净的。
		select {
		case h.hub.unregister <- c:
		case <-h.hub.done:
		}
		c.close()
	}()

	if h.cfg.MaxMessageSize > 0 {
		// 限制单条消息大小：否则一个客户端就能用超大帧把服务端内存吃光。
		c.conn.SetReadLimit(h.cfg.MaxMessageSize)
	}
	if h.cfg.PongWait > 0 {
		_ = c.conn.SetReadDeadline(deadline(h.cfg.PongWait))
		c.conn.SetPongHandler(func(string) error {
			return c.conn.SetReadDeadline(deadline(h.cfg.PongWait))
		})
	}

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("用户 %s 连接异常: %v", c.uid, err)
			}
			return
		}

		msg, err := common.DecodeMessage(data)
		if err != nil {
			c.push(common.ErrorMessage("消息格式错误：需要 JSON 对象").Encode())
			continue
		}
		if msg.Receiver == "" {
			c.push(common.ErrorMessage("缺少 receiver").Encode())
			continue
		}
		if msg.Content == "" {
			c.push(common.ErrorMessage("消息内容不能为空").Encode())
			continue
		}

		// 发送者以连接身份为准，客户端传的 sender 一律忽略，避免伪造他人身份。
		msg.Sender = c.uid
		msg.Time = common.Now()

		select {
		case h.hub.deliver <- msg:
		case <-h.hub.done:
			return
		}
	}
}

// writePump 写循环：唯一的写方，负责把 send 里的报文写到连接上，并按周期发 ping。
func (h *Handler) writePump(c *Client) {
	// 周期 <=0 时 ping 保持 nil，在 select 上等于永久阻塞，心跳随之关闭。
	var ping <-chan time.Time
	if h.cfg.PingPeriod > 0 {
		t := time.NewTicker(h.cfg.PingPeriod)
		defer t.Stop()
		ping = t.C
	}

	for {
		select {
		case data := <-c.send:
			_ = c.conn.SetWriteDeadline(deadline(h.cfg.WriteWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("发送给 %s 失败: %v", c.uid, err)
				c.close()
				return
			}

		case <-ping:
			_ = c.conn.SetWriteDeadline(deadline(h.cfg.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.close()
				return
			}

		case <-c.done:
			// 尽力发一个关闭帧，让对端知道是服务端主动断开（而不是网络抖动），
			// 前端据此可以不做自动重连。
			_ = c.conn.SetWriteDeadline(deadline(h.cfg.WriteWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}
	}
}

// deadline 把时长转成写 / 读截止时间；<=0 表示不设限制（零值 Time 即无截止）。
func deadline(d time.Duration) time.Time {
	if d <= 0 {
		return time.Time{}
	}
	return time.Now().Add(d)
}
