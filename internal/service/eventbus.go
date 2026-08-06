package service

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// EventBus 全局事件总线，管理 WebSocket 客户端订阅与事件广播。
type EventBus struct {
	mu       sync.RWMutex
	clients  map[*Client]struct{}
	register chan *Client
	unreg    chan *Client
	broadcast chan *Event
}

// Client 代表一个 WebSocket 客户端连接。
type Client struct {
	ID   string
	Send chan []byte
	bus  *EventBus
}

// Event WebSocket 推送的事件结构。
type Event struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

func NewEventBus() *EventBus {
	bus := &EventBus{
		clients:   make(map[*Client]struct{}),
		register:  make(chan *Client),
		unreg:     make(chan *Client),
		broadcast: make(chan *Event, 64),
	}
	go bus.run()
	return bus
}

// NewClient 创建一个 WebSocket 客户端并注册到事件总线。
func (b *EventBus) NewClient(id string) *Client {
	c := &Client{
		ID:   id,
		Send: make(chan []byte, 32),
		bus:  b,
	}
	b.register <- c
	return c
}

// RemoveClient 从事件总线移除客户端。
func (b *EventBus) RemoveClient(c *Client) {
	b.unreg <- c
}

// Publish 发布事件到所有订阅客户端。
func (b *EventBus) Publish(event *Event) {
	select {
	case b.broadcast <- event:
	default:
		log.Printf("[eventbus] broadcast channel full, dropping event: %s", event.Type)
	}
}

func (b *EventBus) run() {
	for {
		select {
		case client := <-b.register:
			b.mu.Lock()
			b.clients[client] = struct{}{}
			b.mu.Unlock()

		case client := <-b.unreg:
			b.mu.Lock()
			if _, ok := b.clients[client]; ok {
				delete(b.clients, client)
				close(client.Send)
			}
			b.mu.Unlock()

		case event := <-b.broadcast:
			data, err := json.Marshal(event)
			if err != nil {
				log.Printf("[eventbus] marshal event: %v", err)
				continue
			}
			b.mu.RLock()
			for client := range b.clients {
				select {
				case client.Send <- data:
				default:
					// 客户端跟不上，关闭连接
					go func(c *Client) {
						b.unreg <- c
					}(client)
				}
			}
			b.mu.RUnlock()
		}
	}
}

// ---------- 预定义事件类型 ----------

const (
	EventCameraStatus  = "camera_status"
	EventRecordingStatus = "recording_status"
	EventTimeSync      = "time_sync_event"
	EventSystemMetrics = "system_metrics"
)

// PublishCameraStatus 广播摄像头状态变更。
func (b *EventBus) PublishCameraStatus(cameraID uint, name, status string) {
	b.Publish(&Event{
		Type:      EventCameraStatus,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"camera_id": cameraID,
			"name":      name,
			"status":    status,
		},
	})
}

// PublishRecordingStatus 广播录像状态变更。
func (b *EventBus) PublishRecordingStatus(recordingID, cameraID uint, status string) {
	b.Publish(&Event{
		Type:      EventRecordingStatus,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"recording_id": recordingID,
			"camera_id":    cameraID,
			"status":       status,
		},
	})
}

// PublishTimeSync 广播对时事件。
func (b *EventBus) PublishTimeSync(cameraID uint, success bool, message string) {
	b.Publish(&Event{
		Type:      EventTimeSync,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"camera_id": cameraID,
			"success":   success,
			"message":   message,
		},
	})
}

// PublishSystemMetrics 广播系统指标。
func (b *EventBus) PublishSystemMetrics(metrics map[string]interface{}) {
	b.Publish(&Event{
		Type:      EventSystemMetrics,
		Timestamp: time.Now(),
		Data:      metrics,
	})
}
