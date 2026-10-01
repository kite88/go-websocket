package common

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidUID(t *testing.T) {
	ok := []string{"alice", "u-3f2a1b9c", "A_1", "123", strings.Repeat("x", MaxUIDLength)}
	for _, uid := range ok {
		if !ValidUID(uid) {
			t.Errorf("uid %q 应当合法", uid)
		}
	}

	bad := []string{"", "a b", "中文uid", "a.b", "a/b", strings.Repeat("x", MaxUIDLength+1)}
	for _, uid := range bad {
		if ValidUID(uid) {
			t.Errorf("uid %q 应当被拒绝", uid)
		}
	}
}

func TestClip(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"abcdef", 3, "abc..."},
		{"中文测试", 2, "中文..."}, // 按 rune 截断，不会切出半个汉字
		{"abc", 10, "abc"},
		{"abc", 0, ""},
	}
	for _, tc := range cases {
		if got := Clip(tc.in, tc.max); got != tc.want {
			t.Errorf("Clip(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestDecodeMessage(t *testing.T) {
	msg, err := DecodeMessage([]byte(`{"receiver":" bob ","content":" hi ","type":"whatever"}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if msg.Receiver != "bob" || msg.Content != "hi" {
		t.Errorf("首尾空白应当被去掉: %+v", msg)
	}
	if msg.Type != TypeChat {
		t.Errorf("type 应被强制为 %q，实际 %q", TypeChat, msg.Type)
	}

	if _, err := DecodeMessage([]byte("not-json")); err == nil {
		t.Error("非法 JSON 应当返回错误")
	}
	if _, err := DecodeMessage([]byte(`"just-a-string"`)); err == nil {
		t.Error("JSON 字符串不是合法报文，应当返回错误")
	}
}

func TestEnvelopes(t *testing.T) {
	var chat Message
	if err := json.Unmarshal(ChatMessage("alice", "bob", "hi").Encode(), &chat); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if chat.Type != TypeChat || chat.Sender != "alice" || chat.Receiver != "bob" || chat.Content != "hi" {
		t.Errorf("聊天报文不符合预期: %+v", chat)
	}
	if chat.Time == "" {
		t.Error("聊天报文应当带服务端时间戳")
	}

	var e Message
	if err := json.Unmarshal(ErrorMessage("boom").Encode(), &e); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if e.Type != TypeError || e.Content != "boom" {
		t.Errorf("错误报文不符合预期: %+v", e)
	}
}
