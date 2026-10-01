package handle

import (
	"log"
	"sort"
	"sync"

	"github.com/gorilla/websocket"

	"go-websocket/common"
)

// Client 是一条已建立的 WebSocket 连接。
//
// send 是待发送队列：连接上的写操作全部收敛到 writePump 这一个 goroutine，
// 其它 goroutine 只往 send 里投递——gorilla/websocket 明确不支持并发写同一个
// 连接，旧版实现里 Hub 和 ReadMsg 都会直接调 WriteMessage，是实打实的数据竞争。
//
// done 用来通知 writePump 收尾。之所以不用「关闭 send」来通知：关闭之后任何一次
// 投递都会 panic，而投递方可能来自 Hub、也可能来自 readPump，时序无法保证；
// close(done) 由 sync.Once 保证只发生一次，投递方永远不会 panic，最多是白投一次。
type Client struct {
	uid  string
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
	once sync.Once
}

// UID 返回连接对应的用户标识。
func (c *Client) UID() string { return c.uid }

// close 幂等地关闭连接：先通知 writePump 退出，再断开底层 TCP。
func (c *Client) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// push 非阻塞地投递一条报文。队列满或连接已关闭时返回 false。
//
// 用非阻塞投递而不是直接阻塞：慢客户端不能拖住 Hub（它一旦卡住，所有人的
// 消息都投不出去）。丢弃 + 由调用方决定是否踢连接，是这里刻意的取舍。
func (c *Client) push(data []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

// Hub 是连接中心：在单个 goroutine 里维护 uid -> Client 的映射。
//
// 所有会改动映射的操作都通过 channel 投递进来，因此 map 本身不需要加锁，
// 也就不存在「读的时候正好在写」这类问题。旧版把 ClientList 作为全局变量，
// 多个 handler goroutine 并发读写同一个 map，在并发连接下会直接 panic。
type Hub struct {
	clients    map[string]*Client
	register   chan *Client
	unregister chan *Client
	deliver    chan common.Message
	online     chan chan []string
	done       chan struct{}
	once       sync.Once
	wg         sync.WaitGroup
}

// NewHub 创建并启动连接中心。
func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		// 投递通道带缓冲：WebSocket 读循环与 Hub 之间只隔这一层，缓冲能吸收
		// 突发消息，避免读循环在等待 Hub 处理时被阻塞。
		deliver: make(chan common.Message, 256),
		online:  make(chan chan []string),
		done:    make(chan struct{}),
	}
	h.wg.Add(1)
	go h.run()
	return h
}

// Close 关闭连接中心并断开全部客户端；重复调用安全。
func (h *Hub) Close() {
	h.once.Do(func() {
		close(h.done)
		h.wg.Wait()
	})
}

// Online 返回当前在线用户（按 uid 升序）；连接中心已停止时返回 nil。
func (h *Hub) Online() []string {
	reply := make(chan []string, 1)
	select {
	case h.online <- reply:
	case <-h.done:
		return nil
	}
	select {
	case uids := <-reply:
		return uids
	case <-h.done:
		return nil
	}
}

// run 是连接中心的事件循环，也是唯一改动 clients 的地方。
func (h *Hub) run() {
	defer h.wg.Done()
	for {
		select {
		case <-h.done:
			// 停机：逐个断开，让客户端的 writePump / readPump 自行收尾。
			for uid, c := range h.clients {
				delete(h.clients, uid)
				c.close()
			}
			return

		case c := <-h.register:
			// 同一个 uid 重复连接时替换旧连接：否则后来者会把消息发给自己
			// 那个已经不在使用的连接，前端表现为「消息发出去没人收到」。
			if old, ok := h.clients[c.uid]; ok && old != c {
				delete(h.clients, c.uid)
				old.close()
				log.Printf("用户 %s 重复连接，旧连接已被替换", c.uid)
			}
			h.clients[c.uid] = c
			log.Printf("用户加入 %s（在线 %d）", c.uid, len(h.clients))

		case c := <-h.unregister:
			if cur, ok := h.clients[c.uid]; ok && cur == c {
				delete(h.clients, c.uid)
				c.close()
				log.Printf("用户退出 %s（在线 %d）", c.uid, len(h.clients))
			}

		case msg := <-h.deliver:
			h.deliverMessage(msg)

		case reply := <-h.online:
			reply <- h.onlineUIDs()
		}
	}
}

// deliverMessage 把消息投给接收方；接收方不在线或队列已满时回一条错误给发送方。
func (h *Hub) deliverMessage(msg common.Message) {
	target, ok := h.clients[msg.Receiver]
	if !ok {
		h.notify(msg.Sender, common.ErrorMessage("接收人 "+msg.Receiver+" 不在线"))
		return
	}
	if target.push(msg.Encode()) {
		log.Printf("消息投递 %s -> %s: %s", msg.Sender, msg.Receiver, common.Clip(msg.Content, 50))
		return
	}
	// 队列满：对端已经不读了，再留在这里只会让后续消息一起堆积，直接断开。
	delete(h.clients, msg.Receiver)
	target.close()
	log.Printf("用户 %s 的发送队列已满，连接被关闭", msg.Receiver)
	h.notify(msg.Sender, common.ErrorMessage("接收人 "+msg.Receiver+" 的发送队列已满，连接已断开"))
}

// notify 把一条服务端提示回给指定用户；对方不在线或队列满时静默丢弃。
func (h *Hub) notify(uid string, msg common.Message) {
	if c, ok := h.clients[uid]; ok {
		c.push(msg.Encode())
	}
}

// onlineUIDs 收集当前在线 uid 并排序，保证接口输出稳定（便于测试与前端展示）。
func (h *Hub) onlineUIDs() []string {
	uids := make([]string, 0, len(h.clients))
	for uid := range h.clients {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	return uids
}
