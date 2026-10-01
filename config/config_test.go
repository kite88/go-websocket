package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// writeIni 在临时目录里落一份配置文件，用于验证外部文件覆盖内嵌模板。
func writeIni(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env.ini")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEmbeddedReleaseTemplate(t *testing.T) {
	t.Setenv(EnvKey, "")
	if err := Init(""); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}

	if got := Source(); got != "内嵌 env.ini.release" {
		t.Errorf("Source = %q, want 内嵌 env.ini.release", got)
	}
	if got := GetString("server.http_port", ""); got != "8090" {
		t.Errorf("server.http_port = %q, want 8090", got)
	}
	if got := EnvMode(); got != gin.ReleaseMode {
		t.Errorf("EnvMode = %q, want release", got)
	}
}

func TestEnvKeyChoosesTemplate(t *testing.T) {
	t.Setenv(EnvKey, "debug")
	if err := Init(""); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if got := EnvMode(); got != gin.DebugMode {
		t.Errorf("EnvMode = %q, want debug", got)
	}
}

// TestEnvKeyFallback 环境名写错时回落 release，而不是让 gin.SetMode panic。
func TestEnvKeyFallback(t *testing.T) {
	t.Setenv(EnvKey, "nonsense")
	if err := Init(""); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if got := Source(); got != "内嵌 env.ini.release" {
		t.Errorf("Source = %q, want 内嵌 env.ini.release", got)
	}
}

func TestExplicitFileOverridesDefaults(t *testing.T) {
	path := writeIni(t, "env_mode = test\n\n[server]\nhttp_port = 1234\n\n[websocket]\npong_wait = 3s\n")

	if err := Init(path); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if got := Source(); got != path {
		t.Errorf("Source = %q, want %q", got, path)
	}
	if got := EnvMode(); got != gin.TestMode {
		t.Errorf("EnvMode = %q, want test", got)
	}
	if got := GetInt("server.http_port", 0); got != 1234 {
		t.Errorf("server.http_port = %d, want 1234", got)
	}
	if got := GetDuration("websocket.pong_wait", time.Second); got != 3*time.Second {
		t.Errorf("websocket.pong_wait = %v, want 3s", got)
	}
}

// TestExplicitFileMustExist 显式指定的文件不存在时必须报错，
// 否则「改的配置文件没生效」会被悄悄吞掉。
func TestExplicitFileMustExist(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "nope.ini")); err == nil {
		t.Error("指定了不存在的配置文件应当报错")
	}
}

// TestInvalidEnvModeFallsBack 非法 env_mode 回落 release，避免 gin.SetMode panic。
func TestInvalidEnvModeFallsBack(t *testing.T) {
	path := writeIni(t, "env_mode = 乱写\n")

	if err := Init(path); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if got := EnvMode(); got != gin.ReleaseMode {
		t.Errorf("EnvMode = %q, want release", got)
	}
}

// TestAccessorsFallback 取值失败时必须返回默认值，绝不 panic。
func TestAccessorsFallback(t *testing.T) {
	t.Setenv(EnvKey, "")
	if err := Init(""); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}

	if got := GetInt("server.http_port", -1); got != 8090 {
		t.Errorf("已有配置项取值 = %d, want 8090", got)
	}
	if got := GetInt("no.such_key", 42); got != 42 {
		t.Errorf("缺失项 GetInt = %d, want 42", got)
	}
	if got := GetInt64("websocket.max_message_size", 0); got != 4096 {
		t.Errorf("GetInt64 = %d, want 4096", got)
	}
	if got := GetInt64("no.such_key", 7); got != 7 {
		t.Errorf("缺失项 GetInt64 = %d, want 7", got)
	}
	if got := GetBool("websocket.allow_all_origins", true); got != false {
		t.Errorf("GetBool = %v, want false", got)
	}
	if got := GetBool("no.such_key", true); got != true {
		t.Errorf("缺失项 GetBool = %v, want true", got)
	}
	if got := GetDuration("websocket.pong_wait", time.Second); got != 60*time.Second {
		t.Errorf("GetDuration = %v, want 60s", got)
	}
	if got := GetDuration("no.such_key", 3*time.Second); got != 3*time.Second {
		t.Errorf("缺失项 GetDuration = %v, want 3s", got)
	}
	if got := GetString("no.such_key", "x"); got != "x" {
		t.Errorf("缺失项 GetString = %q, want x", got)
	}
	if got := GetValue("no.such_key"); got != "" {
		t.Errorf("缺失项 GetValue = %q, want 空串", got)
	}
}
