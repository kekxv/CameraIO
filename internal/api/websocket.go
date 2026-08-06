package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"CameraIO/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有跨域来源
	},
}

func (h *Handler) WebSocketSystem(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	clientID := c.Query("client_id")
	if clientID == "" {
		clientID = conn.RemoteAddr().String()
	}

	client := h.eventBus.NewClient(clientID)

	// 发送欢迎消息
	welcome := service.Event{
		Type:      "connected",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"client_id": clientID,
			"message":   "connected to CameraIO event stream",
		},
	}
	welcomeData, _ := json.Marshal(welcome)
	conn.WriteMessage(websocket.TextMessage, welcomeData)

	// 启动读写 goroutine
	done := make(chan struct{})

	// 读 goroutine（处理客户端消息，如 ping/pong）
	go func() {
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
		close(done)
	}()

	// 写 goroutine（从 EventBus 接收事件并发送给客户端）
	go func() {
		ticker := time.NewTicker(30 * time.Second) // 定期 ping
		defer ticker.Stop()
		defer conn.Close()
		for {
			select {
			case <-done:
				h.eventBus.RemoveClient(client)
				return
			case msg, ok := <-client.Send:
				if !ok {
					conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					h.eventBus.RemoveClient(client)
					return
				}
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					h.eventBus.RemoveClient(client)
					return
				}
			}
		}
	}()
}
