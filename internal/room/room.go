package room

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"fitnes-platform/internal/model"

	"github.com/gorilla/websocket"
)

var ErrRoomFull = errors.New("room full")

var Upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

type Room struct {
	clients  map[*Client]struct{}
	messages []Message
}

type Client struct {
	id     string
	roomID string
	user   *model.User
	conn   *websocket.Conn
	send   chan []byte
	hub    *Hub
	once   sync.Once
}

type Participant struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type Message struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	SignalType   string          `json:"signalType,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
	SenderID     string          `json:"senderId,omitempty"`
	SenderEmail  string          `json:"senderEmail,omitempty"`
	Participants []Participant   `json:"participants,omitempty"`
	History      []Message       `json:"history,omitempty"`
	ClientID     string          `json:"clientId,omitempty"`
	Timestamp    string          `json:"timestamp,omitempty"`
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]*Room)}
}

func NewClient(roomID string, user *model.User, conn *websocket.Conn, hub *Hub) *Client {
	return &Client{
		id:     NewID(),
		roomID: roomID,
		user:   user,
		conn:   conn,
		send:   make(chan []byte, 32),
		hub:    hub,
	}
}

func (c *Client) ID() string {
	return c.id
}

func (c *Client) Send() chan<- []byte {
	return c.send
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("generate id")
	}

	return hex.EncodeToString(b[:])
}

func MustJSON(msg Message) []byte {
	payload, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}

	return payload
}

func (h *Hub) Join(roomID string, c *Client) ([]Participant, []Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room := h.rooms[roomID]
	if room == nil {
		room = &Room{clients: make(map[*Client]struct{})}
		h.rooms[roomID] = room
	}

	if len(room.clients) >= 2 {
		return nil, nil, ErrRoomFull
	}

	room.clients[c] = struct{}{}
	log.Printf("room=%s join user=%s client=%s participants=%d", roomID, c.user.Email, c.id, len(room.clients))
	return room.snapshotParticipants(), room.snapshotHistory(), nil
}

func (h *Hub) Leave(roomID string, c *Client) []Participant {
	h.mu.Lock()
	defer h.mu.Unlock()

	room := h.rooms[roomID]
	if room == nil {
		return nil
	}

	delete(room.clients, c)
	if len(room.clients) == 0 {
		delete(h.rooms, roomID)
		log.Printf("room=%s leave user=%s client=%s participants=0", roomID, c.user.Email, c.id)
		return nil
	}

	log.Printf("room=%s leave user=%s client=%s participants=%d", roomID, c.user.Email, c.id, len(room.clients))
	return room.snapshotParticipants()
}

func (h *Hub) Broadcast(roomID string, message Message, except *Client) {
	payload, err := json.Marshal(message)
	if err != nil {
		log.Printf("marshal socket message: %v", err)
		return
	}

	h.mu.Lock()
	room := h.rooms[roomID]
	if room == nil {
		h.mu.Unlock()
		return
	}

	if message.Type == "chat" {
		room.messages = append(room.messages, message)
		if len(room.messages) > 50 {
			room.messages = room.messages[len(room.messages)-50:]
		}
	}

	clients := make([]*Client, 0, len(room.clients))
	for client := range room.clients {
		if client != except {
			clients = append(clients, client)
		}
	}
	h.mu.Unlock()

	for _, client := range clients {
		log.Printf("room=%s broadcast type=%s from=%s to=%s", roomID, message.Type, message.SenderEmail, client.user.Email)
		select {
		case client.send <- payload:
		default:
			go client.Close()
		}
	}
}

func (h *Hub) BroadcastParticipants(roomID string) {
	h.mu.Lock()
	room := h.rooms[roomID]
	if room == nil {
		h.mu.Unlock()
		return
	}

	participants := room.snapshotParticipants()
	clients := make([]*Client, 0, len(room.clients))
	for client := range room.clients {
		clients = append(clients, client)
	}
	h.mu.Unlock()

	message := Message{
		Type:         "participants",
		Participants: participants,
	}
	payload, err := json.Marshal(message)
	if err != nil {
		log.Printf("marshal participants: %v", err)
		return
	}

	for _, client := range clients {
		select {
		case client.send <- payload:
		default:
			go client.Close()
		}
	}
}

func (r *Room) snapshotParticipants() []Participant {
	participants := make([]Participant, 0, len(r.clients))
	for client := range r.clients {
		participants = append(participants, Participant{
			ID:    client.id,
			Email: client.user.Email,
		})
	}

	sort.Slice(participants, func(i, j int) bool {
		return participants[i].ID < participants[j].ID
	})
	return participants
}

func (r *Room) snapshotHistory() []Message {
	history := make([]Message, len(r.messages))
	copy(history, r.messages)
	return history
}

func (c *Client) ReadPump() {
	defer c.Close()

	for {
		var incoming Message
		if err := c.conn.ReadJSON(&incoming); err != nil {
			return
		}

		switch incoming.Type {
		case "chat":
			text := strings.TrimSpace(incoming.Text)
			if text == "" {
				continue
			}
			log.Printf("room=%s chat from=%s client=%s text=%q", c.roomID, c.user.Email, c.id, text)
			c.hub.Broadcast(c.roomID, Message{
				Type:        "chat",
				Text:        text,
				SenderID:    c.id,
				SenderEmail: c.user.Email,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			}, nil)
		case "signal":
			log.Printf("room=%s signal from=%s client=%s type=%s", c.roomID, c.user.Email, c.id, incoming.SignalType)
			c.hub.Broadcast(c.roomID, Message{
				Type:        "signal",
				SignalType:  incoming.SignalType,
				Data:        incoming.Data,
				SenderID:    c.id,
				SenderEmail: c.user.Email,
			}, c)
		}
	}
}

func (c *Client) WritePump() {
	defer c.Close()

	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (c *Client) Close() {
	c.once.Do(func() {
		_ = c.conn.Close()
		participants := c.hub.Leave(c.roomID, c)
		if len(participants) > 0 {
			c.hub.BroadcastParticipants(c.roomID)
		}
		close(c.send)
	})
}
