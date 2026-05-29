package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var ErrRoomFull = errors.New("room full")

type RoomHub struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

type Room struct {
	clients  map[*Client]struct{}
	messages []socketMessage
}

type Client struct {
	id     string
	roomID string
	user   *User
	conn   *websocket.Conn
	send   chan []byte
	hub    *RoomHub
	once   sync.Once
}

type roomParticipant struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type socketMessage struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	SignalType   string            `json:"signalType,omitempty"`
	Data         json.RawMessage   `json:"data,omitempty"`
	SenderID     string            `json:"senderId,omitempty"`
	SenderEmail  string            `json:"senderEmail,omitempty"`
	Participants []roomParticipant `json:"participants,omitempty"`
	History      []socketMessage   `json:"history,omitempty"`
	ClientID     string            `json:"clientId,omitempty"`
	Timestamp    string            `json:"timestamp,omitempty"`
}

func NewRoomHub() *RoomHub {
	return &RoomHub{rooms: make(map[string]*Room)}
}

func (h *RoomHub) Join(roomID string, c *Client) ([]roomParticipant, []socketMessage, error) {
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

func (h *RoomHub) Leave(roomID string, c *Client) []roomParticipant {
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

func (h *RoomHub) Broadcast(roomID string, message socketMessage, except *Client) {
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
			go client.close()
		}
	}
}

func (h *RoomHub) BroadcastParticipants(roomID string) {
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

	message := socketMessage{
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
			go client.close()
		}
	}
}

func (r *Room) snapshotParticipants() []roomParticipant {
	participants := make([]roomParticipant, 0, len(r.clients))
	for client := range r.clients {
		participants = append(participants, roomParticipant{
			ID:    client.id,
			Email: client.user.Email,
		})
	}

	sort.Slice(participants, func(i, j int) bool {
		return participants[i].ID < participants[j].ID
	})
	return participants
}

func (r *Room) snapshotHistory() []socketMessage {
	history := make([]socketMessage, len(r.messages))
	copy(history, r.messages)
	return history
}

func (a *App) roomSocketHandler(w http.ResponseWriter, r *http.Request) {
	user := a.mustCurrentUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	roomID := chi.URLParam(r, "id")
	if _, err := a.store.FindTrainingByID(r.Context(), roomID); err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	client := &Client{
		id:     newID(),
		roomID: roomID,
		user:   user,
		conn:   conn,
		send:   make(chan []byte, 32),
		hub:    a.hub,
	}

	participants, history, err := a.hub.Join(roomID, client)
	if err != nil {
		_ = conn.WriteJSON(socketMessage{Type: "room-full"})
		_ = conn.Close()
		return
	}

	client.send <- mustJSON(socketMessage{
		Type:         "welcome",
		ClientID:     client.id,
		Participants: participants,
		History:      history,
	})
	log.Printf("room=%s welcome user=%s client=%s history=%d participants=%d", roomID, user.Email, client.id, len(history), len(participants))
	a.hub.BroadcastParticipants(roomID)

	go client.writePump()
	client.readPump()
}

func (c *Client) readPump() {
	defer c.close()

	for {
		var incoming socketMessage
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
			c.hub.Broadcast(c.roomID, socketMessage{
				Type:        "chat",
				Text:        text,
				SenderID:    c.id,
				SenderEmail: c.user.Email,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			}, nil)
		case "signal":
			log.Printf("room=%s signal from=%s client=%s type=%s", c.roomID, c.user.Email, c.id, incoming.SignalType)
			c.hub.Broadcast(c.roomID, socketMessage{
				Type:        "signal",
				SignalType:  incoming.SignalType,
				Data:        incoming.Data,
				SenderID:    c.id,
				SenderEmail: c.user.Email,
			}, c)
		}
	}
}

func (c *Client) writePump() {
	defer c.close()

	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (c *Client) close() {
	c.once.Do(func() {
		_ = c.conn.Close()
		participants := c.hub.Leave(c.roomID, c)
		if len(participants) > 0 {
			c.hub.BroadcastParticipants(c.roomID)
		}
		close(c.send)
	})
}

func mustJSON(msg socketMessage) []byte {
	payload, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}

	return payload
}
