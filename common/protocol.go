package common

import "encoding/json"

// 报文类型。客户端上行只有 chat（缺省即可，与旧版前端保持兼容）；
// 服务端下行会带明确的 type，便于前端区分渲染方式。
const (
	// TypeChat 是客户端之间的普通聊天消息。
	TypeChat = "chat"
	// TypeError 是服务端回给单个客户端的错误提示，content 即原因。
	TypeError = "error"
)

// Message 是 WebSocket 上唯一的报文结构，上下行共用。
//
// 字段全部带 omitempty：错误提示不需要 receiver，聊天消息不需要额外字段，
// 报文越短越好。与旧版兼容的最小集合是 receiver + content，sender 由服务端
// 强制覆盖，客户端传什么都不作数。
type Message struct {
	Type     string `json:"type,omitempty"`
	Sender   string `json:"sender,omitempty"`
	Receiver string `json:"receiver,omitempty"`
	Content  string `json:"content,omitempty"`
	Time     string `json:"time,omitempty"`
}

// Encode 把报文序列化成待发送的字节。
//
// 结构体里只有字符串字段，json.Marshal 不会失败；真失败了返回 nil，
// 调用方按「消息发不出去」处理即可，不需要为此中断连接。
func (m Message) Encode() []byte {
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return data
}

// DecodeMessage 解析客户端上行报文。
//
// 严格要求是 JSON 对象：旧版直接用 json.Unmarshal 且忽略错误，客户端发个
// 空字符串就能让服务端往 Hub 里塞一条空消息。这里把错误交给调用方回显。
func DecodeMessage(data []byte) (Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, err
	}
	msg.Type = TypeChat
	msg.Content = TrimSpace(msg.Content)
	msg.Receiver = TrimSpace(msg.Receiver)
	return msg, nil
}

// ErrorMessage 生成一条错误提示报文（只发给触发它的那个客户端）。
func ErrorMessage(content string) Message {
	return Message{
		Type:    TypeError,
		Content: content,
		Time:    Now(),
	}
}

// ChatMessage 生成一条转发给接收方的聊天报文，发送者与时间戳由服务端填写，
// 避免客户端伪造身份或时间。
func ChatMessage(sender, receiver, content string) Message {
	return Message{
		Type:     TypeChat,
		Sender:   sender,
		Receiver: receiver,
		Content:  content,
		Time:     Now(),
	}
}
