package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"go-websocket/common"
	"go-websocket/handle"
	"go-websocket/router"
)

// newServer 起一个真实监听端口的测试服务器：WebSocket 握手要走 Hijack，
// httptest.NewRecorder 那种「假响应」做不到，必须有个真服务器。
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h := handle.New(testConfig())
	engine, err := router.R(gin.TestMode, os.DirFS(".."), os.DirFS(".."), h)
	if err != nil {
		t.Fatalf("创建路由失败: %v", err)
	}

	srv := httptest.NewServer(engine)
	t.Cleanup(func() {
		srv.Close()
		h.Close()
	})
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

// dial 以指定 uid 建立连接；失败时把 HTTP 状态码一并带出来，便于定位。
func dial(t *testing.T, srv *httptest.Server, uid string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(srv)+"?uid="+url.QueryEscape(uid), nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("连接 %q 失败: %v（状态码 %d）", uid, err, status)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readMessage(t *testing.T, conn *websocket.Conn) common.Message {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("读取消息失败: %v", err)
	}
	var msg common.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("解析消息失败: %v（原文 %s）", err, data)
	}
	return msg
}

func writeJSON(t *testing.T, conn *websocket.Conn, payload any) {
	t.Helper()
	if err := conn.WriteJSON(payload); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
}

// online 轮询等待在线人数达到 want 后返回列表。
// 注册是异步完成的（handler 把连接投给 Hub 之后才返回），不能拨完号就断言。
func online(t *testing.T, srv *httptest.Server, want int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var users []string
	for time.Now().Before(deadline) {
		resp, err := http.Get(srv.URL + "/api/online")
		if err == nil {
			var out struct {
				Users []string `json:"users"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&out)
			_ = resp.Body.Close()
			if decodeErr == nil {
				users = out.Users
				if len(users) == want {
					return users
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("在线人数未达到 %d，当前为 %v", want, users)
	return nil
}

func TestChatForwarding(t *testing.T) {
	srv := newServer(t)
	alice := dial(t, srv, "alice")
	bob := dial(t, srv, "bob")
	online(t, srv, 2)

	writeJSON(t, bob, map[string]string{"receiver": "alice", "content": "你好，alice"})

	msg := readMessage(t, alice)
	if msg.Type != common.TypeChat {
		t.Errorf("type = %q, want %q", msg.Type, common.TypeChat)
	}
	if msg.Sender != "bob" || msg.Receiver != "alice" || msg.Content != "你好，alice" {
		t.Errorf("转发内容不符合预期: %+v", msg)
	}
	if msg.Time == "" {
		t.Error("服务端应补上时间戳")
	}
}

// TestSenderCannotBeForged 客户端自称 sender=alice，服务端必须以连接身份覆盖它，
// 否则任何人都能冒充别人发消息。
func TestSenderCannotBeForged(t *testing.T) {
	srv := newServer(t)
	alice := dial(t, srv, "alice")
	bob := dial(t, srv, "bob")
	online(t, srv, 2)

	writeJSON(t, bob, map[string]string{"receiver": "alice", "sender": "alice", "content": "我是 alice"})

	if msg := readMessage(t, alice); msg.Sender != "bob" {
		t.Errorf("sender 应被服务端覆盖为 bob，实际 %q", msg.Sender)
	}
}

func TestOfflineReceiver(t *testing.T) {
	srv := newServer(t)
	alice := dial(t, srv, "alice")
	online(t, srv, 1)

	writeJSON(t, alice, map[string]string{"receiver": "nobody", "content": "在吗"})

	msg := readMessage(t, alice)
	if msg.Type != common.TypeError || !strings.Contains(msg.Content, "不在线") {
		t.Errorf("接收人不在线时应收到错误提示: %+v", msg)
	}
}

// TestBadMessages 覆盖三类非法上行：非 JSON、缺 receiver、内容为空。
// 旧实现里这三种情况都会被静默塞进消息通道。
func TestBadMessages(t *testing.T) {
	srv := newServer(t)
	alice := dial(t, srv, "alice")
	online(t, srv, 1)

	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"非 JSON", "not-json", "JSON"},
		{"缺 receiver", `{"content":"hi"}`, "receiver"},
		{"空内容", `{"receiver":"bob","content":"   "}`, "内容"},
	}

	for _, tc := range cases {
		if err := alice.WriteMessage(websocket.TextMessage, []byte(tc.payload)); err != nil {
			t.Fatalf("%s: 发送失败: %v", tc.name, err)
		}
		msg := readMessage(t, alice)
		if msg.Type != common.TypeError || !strings.Contains(msg.Content, tc.want) {
			t.Errorf("%s: 期望错误提示含 %q，实际 %+v", tc.name, tc.want, msg)
		}
	}
}

// TestInvalidUID 非法 uid 必须在升级之前以 400 拒掉。
func TestInvalidUID(t *testing.T) {
	srv := newServer(t)

	for _, uid := range []string{"", "a b", "中文uid", strings.Repeat("x", common.MaxUIDLength+1)} {
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL(srv)+"?uid="+url.QueryEscape(uid), nil)
		if err == nil {
			_ = conn.Close()
			t.Errorf("uid %q 不应连接成功", uid)
			continue
		}
		if resp == nil || resp.StatusCode != http.StatusBadRequest {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Errorf("uid %q 状态码 = %d, want 400", uid, status)
		}
	}
}

// TestDuplicateUIDReplacesOld 同一 uid 再来一条连接时，旧连接应被服务端断开，
// 否则消息会投递到那条已经没人看的连接上，前端表现为「发出去了但对方收不到」。
func TestDuplicateUIDReplacesOld(t *testing.T) {
	srv := newServer(t)
	first := dial(t, srv, "dup")
	online(t, srv, 1)

	second := dial(t, srv, "dup")
	online(t, srv, 1)

	_ = first.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := first.ReadMessage(); err == nil {
		t.Error("同一 uid 重复连接时，旧连接应被服务端关闭")
	}

	// 新连接必须仍然可用
	writeJSON(t, second, map[string]string{"receiver": "nobody", "content": "hi"})
	if msg := readMessage(t, second); msg.Type != common.TypeError {
		t.Errorf("新连接应能正常收发，实际 %+v", msg)
	}
}

// TestReconnectAfterClose 断开后可以立刻用同一 uid 重连（不会被残留状态挡住）。
func TestReconnectAfterClose(t *testing.T) {
	srv := newServer(t)
	first := dial(t, srv, "carol")
	online(t, srv, 1)

	if err := first.Close(); err != nil {
		t.Fatalf("关闭连接失败: %v", err)
	}
	online(t, srv, 0)

	second := dial(t, srv, "carol")
	online(t, srv, 1)

	writeJSON(t, second, map[string]string{"receiver": "nobody", "content": "回来了"})
	if msg := readMessage(t, second); msg.Type != common.TypeError {
		t.Errorf("重连后应能正常收发，实际 %+v", msg)
	}
}
