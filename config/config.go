// Package config 负责加载与解析应用配置（INI 格式）。
//
// 配置来源按优先级从高到低：
//  1. -config 命令行参数 / GWS_CONFIG 环境变量指定的 ini 文件（指定了就必须存在）；
//  2. 工作目录下的 env.ini；
//  3. 工作目录下 config/env.ini；
//  4. 二进制内嵌的 env.ini.<env>，<env> 由 GWS_ENV 环境变量指定，默认 release；
//  5. 二进制内嵌的 env.ini.release（兜底）。
//
// 为什么把四份环境模板都内嵌进二进制：只 embed env.ini 的话，而 env.ini 被
// .gitignore 排除、仓库里并不存在，clone 下来直接 go build 会报
// "pattern env.ini: no matching files found"，必须先把 env.ini.local 改名成
// env.ini 才能编译。改成内嵌模板后，源码开箱即可编译，Docker 构建也无需再
// 依赖 mv 改名的技巧，同时仍然支持用外部 env.ini 覆盖内嵌默认值。
package config

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/ini.v1"
)

//go:embed env.ini.debug env.ini.local env.ini.release env.ini.test
var embedded embed.FS

const (
	// EnvKey 环境变量名：选择内嵌的环境模板（debug / local / release / test）。
	EnvKey = "GWS_ENV"
	// PathKey 环境变量名：指定外部配置文件路径，等价于 -config 参数。
	PathKey = "GWS_CONFIG"

	// defaultEnv 内嵌模板的默认环境。
	defaultEnv = gin.ReleaseMode
	// fallbackTemplate 兜底模板，保证任何情况下都能拿到一份可用配置。
	fallbackTemplate = "env.ini.release"
	// externalFile 约定俗成的本地配置文件；仓库只提供 env.ini.<env> 模板。
	externalFile = "env.ini"
)

var (
	conf   *ini.File
	source string
)

// init 用自动发现的配置初始化一次，让包变量在 import 阶段就能取到值；
// 失败时仅降级为「无配置」，错误留给 Init 上报。
func init() {
	_ = Init("")
}

// Init 加载配置。explicitPath 非空时只加载该文件，失败直接返回错误；
// 为空时先看 GWS_CONFIG，再按包注释里的优先级自动查找，正常情况下不会失败。
func Init(explicitPath string) error {
	if explicitPath == "" {
		// GWS_CONFIG 与 -config 等价，方便容器 / systemd 这类不好传命令行参数的场景。
		explicitPath = strings.TrimSpace(os.Getenv(PathKey))
	}
	if explicitPath != "" {
		cfg, err := loadFile(explicitPath)
		if err != nil {
			return fmt.Errorf("加载配置文件失败: %w", err)
		}
		conf, source = cfg, explicitPath
		return nil
	}

	for _, path := range []string{externalFile, filepath.Join("config", externalFile)} {
		if !isRegularFile(path) {
			continue
		}
		cfg, err := loadFile(path)
		if err != nil {
			return fmt.Errorf("加载配置文件失败: %w", err)
		}
		conf, source = cfg, path
		return nil
	}

	name := embeddedTemplate()
	data, err := embedded.ReadFile(name)
	if err != nil {
		return fmt.Errorf("读取内嵌配置 %s 失败: %w", name, err)
	}
	cfg, err := ini.Load(data)
	if err != nil {
		return fmt.Errorf("解析内嵌配置 %s 失败: %w", name, err)
	}
	conf, source = cfg, "内嵌 "+name
	return nil
}

// Source 返回当前配置的来源，便于启动日志中排查「改的配置文件没生效」这类问题。
func Source() string { return source }

// EnvMode 返回 gin 运行模式。取值非法时回落到 release，避免 gin.SetMode panic。
func EnvMode() string {
	switch strings.ToLower(GetValue("env_mode")) {
	case gin.DebugMode:
		return gin.DebugMode
	case gin.TestMode:
		return gin.TestMode
	case gin.ReleaseMode:
		return gin.ReleaseMode
	default:
		return gin.ReleaseMode
	}
}

// GetValue 返回指定配置项的值，不存在或为空时返回空串。
// key 支持「节.键」写法，如 env_mode、server.http_port、websocket.ping_period。
func GetValue(key string) string {
	v, _ := lookup(key)
	return v
}

// GetString 返回字符串配置，缺失或为空时返回 def。
func GetString(key, def string) string {
	if v, ok := lookup(key); ok {
		return v
	}
	return def
}

// GetInt 返回整型配置，缺失或解析失败时返回 def。
func GetInt(key string, def int) int {
	v, ok := lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// GetInt64 返回 64 位整型配置，缺失或解析失败时返回 def。
func GetInt64(key string, def int64) int64 {
	v, ok := lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// GetBool 返回布尔配置，缺失或解析失败时返回 def。
func GetBool(key string, def bool) bool {
	v, ok := lookup(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// GetDuration 返回时长配置（如 10s / 2m），缺失或解析失败时返回 def。
func GetDuration(key string, def time.Duration) time.Duration {
	v, ok := lookup(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// lookup 按键取值：key 里最后一个点之前的部分视为节名，之后的部分视为键名。
func lookup(key string) (string, bool) {
	if conf == nil {
		return "", false
	}
	section, name := "", key
	if i := strings.LastIndex(key, "."); i >= 0 {
		section, name = key[:i], key[i+1:]
	}
	k := conf.Section(section).Key(name)
	if k == nil {
		return "", false
	}
	v := strings.TrimSpace(k.Value())
	if v == "" {
		return "", false
	}
	return v, true
}

// embeddedTemplate 返回应当使用的内嵌模板名，环境非法时回落到 release。
func embeddedTemplate() string {
	env := strings.ToLower(strings.TrimSpace(os.Getenv(EnvKey)))
	if env == "" {
		env = defaultEnv
	}
	name := externalFile + "." + env
	if _, err := embedded.Open(name); err != nil {
		return fallbackTemplate
	}
	return name
}

func loadFile(path string) (*ini.File, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func isRegularFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}
