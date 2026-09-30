package ws

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type commandMessage struct {
	Action  string `json:"action"`
	Channel string `json:"channel"`
}

type broadcastMessage struct {
	Channel string          `json:"channel"`
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data"`
}

type client struct {
	conn *websocket.Conn
	send chan broadcastMessage
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
	clients    map[*client]bool
	channels   map[channelKey]map[*client]bool
	broadcast  chan broadcastEnvelope
	register   chan *client
	unregister chan *client
	subscribe  chan subscription
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*client]bool),
		channels:   make(map[channelKey]map[*client]bool),
		broadcast:  make(chan broadcastEnvelope),
		register:   make(chan *client),
		unregister: make(chan *client),
		subscribe:  make(chan subscription),
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

func (h *Hub) ServeAppHTTP(appID string, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to set websocket upgrade:", err)
		return
	}

	client := &client{conn: conn, send: make(chan broadcastMessage, 256)}
	h.register <- client
	go h.writeMessages(client)
	defer func() {
		h.unregister <- client
		conn.Close()
	}()

	for {
		var command commandMessage
		if err := conn.ReadJSON(&command); err != nil {
			return
		}

		switch command.Action {
		case "subscribe":
			if command.Channel != "" {
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