package ws

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type commandMessage struct {
	Action  string `json:"action"`
	Channel string `json:"channel"`
	Payload string `json:"payload"`
}

type broadcastMessage struct {
	Channel string `json:"channel"`
	Data    string `json:"data"`
}

type client struct {
	conn *websocket.Conn
	send chan broadcastMessage
}

type subscription struct {
	client  *client
	channel string
}

type Hub struct {
	clients    map[*client]bool
	channels   map[string]map[*client]bool
	broadcast  chan broadcastMessage
	register   chan *client
	unregister chan *client
	subscribe  chan subscription
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*client]bool),
		channels:   make(map[string]map[*client]bool),
		broadcast:  make(chan broadcastMessage),
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
			if h.channels[request.channel] == nil {
				h.channels[request.channel] = make(map[*client]bool)
			}
			h.channels[request.channel][request.client] = true
			log.Printf("Client subscribed to channel: %s", request.channel)

		case message := <-h.broadcast:
			for subscriber := range h.channels[message.Channel] {
				select {
				case subscriber.send <- message:
				default:
					h.removeClient(subscriber)
				}
			}
		}
	}
}

func (h *Hub) Publish(channel, data string) {
	h.broadcast <- broadcastMessage{Channel: channel, Data: data}
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

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
			h.subscribe <- subscription{client: client, channel: command.Channel}
		case "publish":
			h.Publish(command.Channel, command.Payload)
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