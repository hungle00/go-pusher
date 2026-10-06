package ws

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxWebSocketMessageBytes = 64 << 10
	maxCommandsPerWindow     = 20
	commandRateWindow        = time.Second
)

type commandMessage struct {
	Action  string `json:"action"`
	Channel string `json:"channel"`
	Auth    string `json:"auth"`
}

type broadcastMessage struct {
	Channel string          `json:"channel"`
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data"`
}

type client struct {
	conn     *websocket.Conn
	appID    string
	socketID string
	send     chan broadcastMessage
}

type commandRateLimiter struct {
	windowStart  time.Time
	commandCount int
}

func (limiter *commandRateLimiter) allow(now time.Time) bool {
	if limiter.windowStart.IsZero() || now.Sub(limiter.windowStart) >= commandRateWindow {
		limiter.windowStart = now
		limiter.commandCount = 0
	}
	if limiter.commandCount >= maxCommandsPerWindow {
		return false
	}
	limiter.commandCount++
	return true
}

type subscription struct {
	client  *client
	appID   string
	channel string
}

type channelKey struct {
	appID   string
	channel string
}

type Hub struct {
	clients          map[*client]bool
	channels         map[channelKey]map[*client]bool
	broadcast        chan broadcastEnvelope
	register         chan *client
	unregister       chan *client
	subscribe        chan subscription
	socketLookups    chan socketLookup
	channelSnapshots chan channelSnapshotRequest
}

type socketLookup struct {
	socketID string
	appID    string
	result   chan bool
}

type ChannelStats struct {
	Name            string `json:"name"`
	SubscriberCount int    `json:"subscriber_count"`
}

type channelSnapshotRequest struct {
	appID  string
	result chan []ChannelStats
}

func NewHub() *Hub {
	return &Hub{
		clients:          make(map[*client]bool),
		channels:         make(map[channelKey]map[*client]bool),
		broadcast:        make(chan broadcastEnvelope),
		register:         make(chan *client),
		unregister:       make(chan *client),
		subscribe:        make(chan subscription),
		socketLookups:    make(chan socketLookup),
		channelSnapshots: make(chan channelSnapshotRequest),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			h.removeClient(client)

		case request := <-h.subscribe:
			if !h.clients[request.client] {
				continue
			}
			key := channelKey{appID: request.appID, channel: request.channel}
			if h.channels[key] == nil {
				h.channels[key] = make(map[*client]bool)
			}
			h.channels[key][request.client] = true
			log.Printf("Client subscribed to app %s channel %s", request.appID, request.channel)

		case lookup := <-h.socketLookups:
			valid := false
			for client := range h.clients {
				if client.socketID == lookup.socketID && client.appID == lookup.appID {
					valid = true
					break
				}
			}
			lookup.result <- valid

		case snapshot := <-h.channelSnapshots:
			channels := make([]ChannelStats, 0)
			for key, subscribers := range h.channels {
				if key.appID == snapshot.appID && len(subscribers) > 0 {
					channels = append(channels, ChannelStats{Name: key.channel, SubscriberCount: len(subscribers)})
				}
			}
			sort.Slice(channels, func(i, j int) bool { return channels[i].Name < channels[j].Name })
			snapshot.result <- channels

		case message := <-h.broadcast:
			for subscriber := range h.channels[channelKey{appID: message.appID, channel: message.Channel}] {
				select {
				case subscriber.send <- message.broadcastMessage:
				default:
					h.removeClient(subscriber)
				}
			}
		}
	}
}

func (h *Hub) Publish(appID, channel, event string, data json.RawMessage) {
	h.broadcast <- broadcastEnvelope{appID: appID, broadcastMessage: broadcastMessage{Channel: channel, Event: event, Data: data}}
}

type broadcastEnvelope struct {
	appID string
	broadcastMessage
}

func (h *Hub) removeClient(client *client) {
	if !h.clients[client] {
		return
	}
	delete(h.clients, client)
	close(client.send)
	for channel, subscribers := range h.channels {
		delete(subscribers, client)
		if len(subscribers) == 0 {
			delete(h.channels, channel)
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h *Hub) SocketBelongs(appID, socketID string) bool {
	result := make(chan bool)
	h.socketLookups <- socketLookup{appID: appID, socketID: socketID, result: result}
	return <-result
}

func (h *Hub) ActiveChannels(appID string) []ChannelStats {
	result := make(chan []ChannelStats, 1)
	h.channelSnapshots <- channelSnapshotRequest{appID: appID, result: result}
	return <-result
}

func (h *Hub) ServeAppHTTP(appID string, w http.ResponseWriter, r *http.Request, authorize func(string, string, string) error) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to set websocket upgrade:", err)
		return
	}

	client := &client{conn: conn, appID: appID, socketID: randomSocketID(), send: make(chan broadcastMessage, 256)}
	conn.SetReadLimit(maxWebSocketMessageBytes)
	h.register <- client
	if err := conn.WriteJSON(map[string]string{"event": "connected", "socket_id": client.socketID}); err != nil {
		return
	}
	go h.writeMessages(client)
	defer func() {
		h.unregister <- client
		conn.Close()
	}()

	var commandLimiter commandRateLimiter
	for {
		var command commandMessage
		if err := conn.ReadJSON(&command); err != nil {
			return
		}
		if !commandLimiter.allow(time.Now()) {
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "command rate limit exceeded"),
				time.Now().Add(time.Second),
			)
			return
		}

		switch command.Action {
		case "subscribe":
			if command.Channel != "" {
				if strings.HasPrefix(command.Channel, "private-") {
					if authorize == nil || authorize(client.socketID, command.Channel, command.Auth) != nil {
						select {
						case client.send <- broadcastMessage{Channel: command.Channel, Event: "subscription_error", Data: json.RawMessage(`{"error":"unauthorized"}`)}:
						default:
						}
						continue
					}
				}
				h.subscribe <- subscription{client: client, appID: appID, channel: command.Channel}
			}
		}
	}
}

func (h *Hub) writeMessages(client *client) {
	defer client.conn.Close()
	for message := range client.send {
		if err := client.conn.WriteJSON(message); err != nil {
			return
		}
	}
}

func randomSocketID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}
