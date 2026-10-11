package ws

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	member  *PresenceMember
	result  chan error
}

type PresenceMember struct {
	UserID   string          `json:"user_id"`
	UserInfo json.RawMessage `json:"user_info,omitempty"`
}

type presenceMember struct {
	PresenceMember
	clients map[*client]bool
}

type channelKey struct {
	appID   string
	channel string
}

type Hub struct {
	clients          map[*client]bool
	channels         map[channelKey]map[*client]bool
	presenceMembers  map[channelKey]map[string]*presenceMember
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
		presenceMembers:  make(map[channelKey]map[string]*presenceMember),
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
			err := h.addSubscription(request)
			if request.result != nil {
				request.result <- err
			}

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

func (h *Hub) addSubscription(request subscription) error {
	if !h.clients[request.client] || request.appID != request.client.appID || request.channel == "" {
		return errors.New("socket is not connected to this app")
	}
	isPresence := strings.HasPrefix(request.channel, "presence-")
	if isPresence && (request.member == nil || request.member.UserID == "") {
		return errors.New("presence subscription requires a verified user")
	}
	if !isPresence && request.member != nil {
		return errors.New("user identity is only valid for presence channels")
	}

	key := channelKey{appID: request.appID, channel: request.channel}
	subscribers := h.channels[key]
	if isPresence {
		for userID, member := range h.presenceMembers[key] {
			if member.clients[request.client] && userID != request.member.UserID {
				return errors.New("socket is already subscribed as another presence user")
			}
		}
	}

	existingSubscribers := make([]*client, 0, len(subscribers))
	for subscriber := range subscribers {
		existingSubscribers = append(existingSubscribers, subscriber)
	}
	if subscribers == nil {
		subscribers = make(map[*client]bool)
		h.channels[key] = subscribers
	}
	subscribers[request.client] = true

	ackData := json.RawMessage(`{}`)
	newMember := false
	if isPresence {
		members := h.presenceMembers[key]
		if members == nil {
			members = make(map[string]*presenceMember)
			h.presenceMembers[key] = members
		}
		current := members[request.member.UserID]
		if current == nil {
			current = &presenceMember{PresenceMember: *request.member, clients: make(map[*client]bool)}
			members[request.member.UserID] = current
			newMember = true
		}
		current.clients[request.client] = true

		memberList := make([]PresenceMember, 0, len(members))
		for _, member := range members {
			memberList = append(memberList, member.PresenceMember)
		}
		sort.Slice(memberList, func(i, j int) bool { return memberList[i].UserID < memberList[j].UserID })
		ackData = marshalMessageData(struct {
			Presence struct {
				Count   int              `json:"count"`
				Members []PresenceMember `json:"members"`
			} `json:"presence"`
		}{Presence: struct {
			Count   int              `json:"count"`
			Members []PresenceMember `json:"members"`
		}{Count: len(memberList), Members: memberList}})
	}

	if !h.enqueue(request.client, broadcastMessage{Channel: request.channel, Event: "subscription_succeeded", Data: ackData}) {
		h.removeClient(request.client)
		return nil
	}
	if newMember {
		data := marshalMessageData(request.member)
		var overflow []*client
		for _, subscriber := range existingSubscribers {
			if subscriber == request.client {
				continue
			}
			if member := h.presenceMembers[key][request.member.UserID]; member.clients[subscriber] {
				continue
			}
			if !h.enqueue(subscriber, broadcastMessage{Channel: request.channel, Event: "member_added", Data: data}) {
				overflow = append(overflow, subscriber)
			}
		}
		for _, subscriber := range overflow {
			h.removeClient(subscriber)
		}
	}
	log.Printf("Client subscribed to app %s channel %s", request.appID, request.channel)
	return nil
}

func marshalMessageData(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func (h *Hub) enqueue(client *client, message broadcastMessage) bool {
	select {
	case client.send <- message:
		return true
	default:
		return false
	}
}

func (h *Hub) removeClient(disconnected *client) {
	if !h.clients[disconnected] {
		return
	}
	delete(h.clients, disconnected)
	close(disconnected.send)
	var overflow []*client
	for channel, subscribers := range h.channels {
		if !subscribers[disconnected] {
			continue
		}
		delete(subscribers, disconnected)
		if strings.HasPrefix(channel.channel, "presence-") {
			members := h.presenceMembers[channel]
			for userID, member := range members {
				if !member.clients[disconnected] {
					continue
				}
				delete(member.clients, disconnected)
				if len(member.clients) == 0 {
					delete(members, userID)
					data := marshalMessageData(member.PresenceMember)
					for subscriber := range subscribers {
						if !h.enqueue(subscriber, broadcastMessage{Channel: channel.channel, Event: "member_removed", Data: data}) {
							overflow = append(overflow, subscriber)
						}
					}
				}
				break
			}
		}
		if len(subscribers) == 0 {
			delete(h.channels, channel)
			delete(h.presenceMembers, channel)
		} else if len(h.presenceMembers[channel]) == 0 {
			delete(h.presenceMembers, channel)
		}
	}
	for _, subscriber := range overflow {
		h.removeClient(subscriber)
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

func (h *Hub) ServeAppHTTP(appID string, w http.ResponseWriter, r *http.Request, authorize func(string, string, string) (*PresenceMember, error)) {
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
				isPrivate := strings.HasPrefix(command.Channel, "private-")
				isPresence := strings.HasPrefix(command.Channel, "presence-")
				var member *PresenceMember
				if isPrivate || isPresence {
					var err error
					if authorize == nil {
						err = errors.New("authorization is not configured")
					} else {
						member, err = authorize(client.socketID, command.Channel, command.Auth)
					}
					if err != nil || (isPresence && (member == nil || member.UserID == "")) {
						h.rejectSubscription(client, command.Channel)
						continue
					}
				}
				result := make(chan error, 1)
				h.subscribe <- subscription{client: client, appID: appID, channel: command.Channel, member: member, result: result}
				if err := <-result; err != nil {
					h.rejectSubscription(client, command.Channel)
				}
			}
		}
	}
}

func (h *Hub) rejectSubscription(client *client, channel string) {
	if !h.enqueue(client, broadcastMessage{
		Channel: channel,
		Event:   "subscription_error",
		Data:    json.RawMessage(`{"error":"unauthorized"}`),
	}) {
		h.unregister <- client
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
