// Package test 用进程内路由做集成测试：不占固定端口、不依赖外部环境，
// 直接验证「HTTP 请求 -> 路由 -> handler」这条链路。WebSocket 相关用例在
// ws_test.go 里，需要真实的监听端口（握手要走 Hijack）。
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

	"go-websocket/handle"
	"go-websocket/router"
)

// testConfig 返回一组适合测试的参数：心跳周期拉到 1 小时（测试里不需要真的发
// ping），超时压短，这样用例不必去改配置文件。
func testConfig() handle.Config {
	return handle.Config{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		MaxMessageSize:  4096,
		PingPeriod:      time.Hour,
		PongWait:        5 * time.Second,
		WriteWait:       time.Second,
		SendQueue:       8,
	}
}

// newFixture 建一个进程内的 gin 引擎（用 httptest 直接打，不监听端口）。
func newFixture(t *testing.T) (*gin.Engine, *handle.Handler) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h := handle.New(testConfig())
	t.Cleanup(h.Close)

	// 模板与静态资源取自真实文件：测试目录的上一层就是项目根目录。
	engine, err := router.R(gin.TestMode, os.DirFS(".."), os.DirFS(".."), h)
	if err != nil {
		t.Fatalf("创建路由失败: %v", err)
	}
	return engine, h
}

func do(t *testing.T, engine *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, engine *gin.Engine, path string, params url.Values) *httptest.ResponseRecorder {
	t.Helper()
	target := path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	return do(t, engine, httptest.NewRequest(http.MethodGet, target, nil))
}

func TestIndexPage(t *testing.T) {
	engine, _ := newFixture(t)

	rec := get(t, engine, "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / 状态码 = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "WebSocket 定向聊天") {
		t.Error("首页内容不符合预期")
	}
}

func TestStaticAsset(t *testing.T) {
	engine, _ := newFixture(t)

	for _, path := range []string{
		"/web/static/assets/js/app.js",
		"/web/static/bootstrap/v5.3.5/bootstrap.min.css",
		"/web/static/jquery/jquery-3.7.1.min.js",
		"/favicon.ico",
	} {
		rec := get(t, engine, path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s 状态码 = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "max-age") {
			t.Errorf("GET %s 缺少 Cache-Control: %q", path, got)
		}
	}
}

// TestOnlineEmpty 无连接时在线列表必须是 []（而不是 null）：
// 前端直接对它做 .length / forEach，null 会当场炸掉。
func TestOnlineEmpty(t *testing.T) {
	engine, _ := newFixture(t)

	rec := get(t, engine, "/api/online", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rec.Code)
	}

	var out struct {
		Count int      `json:"count"`
		Users []string `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.Count != 0 || len(out.Users) != 0 {
		t.Errorf("无连接时在线列表应为空: %+v", out)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"users":[]`) {
		t.Errorf("users 应为空数组而不是 null: %s", body)
	}
}

func TestNoRoute(t *testing.T) {
	engine, _ := newFixture(t)

	if rec := get(t, engine, "/api/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("未知接口状态码 = %d, want 404", rec.Code)
	}
	if rec := get(t, engine, "/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("未知页面状态码 = %d, want 404", rec.Code)
	}
	if body := get(t, engine, "/api/nope", nil).Body.String(); !strings.Contains(body, "接口不存在") {
		t.Errorf("未知接口应返回 JSON 错误: %s", body)
	}
}
