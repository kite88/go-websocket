package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"strings"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Client struct {
	ID   string
	Conn *websocket.Conn
}

type Ch struct {
	JoinChan   chan *Client       //用户加入通道
	ExitChan   chan *Client       //用户退出出通道
	MsgChan    chan string        //消息通道
	ClientList map[string]*Client //客户端用户列表
}

var AllCh = Ch{
	JoinChan:   make(chan *Client),
	ExitChan:   make(chan *Client),
	MsgChan:    make(chan string),
	ClientList: make(map[string]*Client),
}

type MsgContent struct {
	Sender   string `json:"sender"`   //发送者
	Receiver string `json:"receiver"` //接收者
	Content  string `json:"content"`  //消息内容
}

func (ch *Ch) Start() {
	for ; ; {
		select {
		case v := <-ch.JoinChan:
			log.Println("用户加入", v.ID)
			AllCh.ClientList[v.ID] = v
		case v := <-ch.ExitChan:
			log.Println("用户退出", v.ID)
			delete(AllCh.ClientList, v.ID)
		case v := <-ch.MsgChan:
			var msgContent MsgContent
			_ = json.Unmarshal([]byte(v), &msgContent)
			for id, conn := range AllCh.ClientList {
				if id == msgContent.Receiver {
					conn.WriteMsg(v)
				}
			}
		}
	}
}

func (c *Client) ReadMsg() {
	defer func() {
		AllCh.ExitChan <- c
		_ = c.Conn.Close()
	}()
	for ; ; {
		_, p, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		var msgContent MsgContent
		_ = json.Unmarshal(p, &msgContent)
		msgContent.Sender = c.ID
		message, _ := json.Marshal(msgContent)
		log.Println("读取到客户端的信息:", string(message))
		AllCh.MsgChan <- string(message)
	}
}

func (c *Client) WriteMsg(message string) {
	err := c.Conn.WriteMessage(websocket.TextMessage, []byte(message))
	if err != nil {
		log.Println(err)
	}
	log.Println("发送到客户端的信息:", message)
}

func handler(w http.ResponseWriter, r *http.Request) {
	go AllCh.Start()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	uid := FormatQuery(fmt.Sprintf("%v", r.URL), "uid")
	c := &Client{
		ID:   uid,
		Conn: conn,
	}
	AllCh.JoinChan <- c
	go c.ReadMsg()
}

func FormatQuery(url string, paramName string) string {
	urls := strings.Split(url, "?")
	strParam := urls[1]
	strArr := strings.Split(strParam, "&")
	OutMap := make(map[string]interface{})
	if strArr[0] != "" && len(strArr) > 0 {
		for _, str := range strArr {
			newArr := strings.Split(str, "=")
			key := newArr[0]
			value := newArr[1]
			OutMap[key] = value
		}
	}
	return fmt.Sprintf("%v", OutMap[paramName])
}

func main() {
	var port = "8090"
	http.HandleFunc("/ws", handler)
	http.Handle("/", http.FileServer(http.Dir("")))
	fmt.Println("http://localhost:" + port)
	http.ListenAndServe(":"+port, nil)
}
