// Package common 放与业务无关的通用工具：uid 校验、日志裁剪与时间格式化。
//
// 协议报文的编解码也在这里（protocol.go），因为 handle、router、test 三层都要用它。
package common

import (
	"strings"
	"time"
)

// MaxUIDLength 是 uid 的最大长度；uid 会出现在前端展示与日志里，太长没必要。
const MaxUIDLength = 64

// Now 返回本地时区的时间戳字符串，用于报文的 time 字段与日志。
func Now() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// ValidUID 校验客户端上报的 uid。
//
// uid 是消息投递的唯一依据，也是日志与前端展示里的身份标识，因此限制成
// 一小撮安全字符：字母、数字、下划线、连字符。这样既能避免「同名不同形」
// 的困惑，也顺手挡掉了往日志里注入换行、往页面里注入 HTML 的输入。
func ValidUID(uid string) bool {
	if len(uid) == 0 || len(uid) > MaxUIDLength {
		return false
	}
	for _, r := range uid {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_',
			r == '-':
		default:
			return false
		}
	}
	return true
}

// Clip 把字符串截断到 max 个字符以内（按 rune 计，中文不会被截成半个字）。
//
// 用途只有一个：写日志。消息内容完全由客户端控制，不截断的话一条超长消息
// 就能把日志文件刷爆。
func Clip(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// TrimSpace 是 strings.TrimSpace 的别名，集中在此是为了让调用方一眼看出
// 「这里的空白容忍度是统一的」。
func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}
