package room

import (
	"encoding/json"
	"log"
	"runtime/debug"
	"sync"

	"collab-editor/pkg/db"

	"github.com/gorilla/websocket"
)

// Operation represents a text operation in the collaborative editor
type Operation struct {
	Type      string `json:"type"`      // "insert", "delete", "retain"
	Position  int    `json:"position"`  // Position in the document
	Content   string `json:"content"`   // Content to insert/delete
	Length    int    `json:"length"`    // Length for retain/delete operations
	ClientID  string `json:"client_id"` // ID of the client that generated this operation
	Timestamp int64  `json:"timestamp"` // Timestamp for ordering operations
}

type MetadataUpdate struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Language  string `json:"language"`
	ClientID  string `json:"client_id"`
	Timestamp int64  `json:"timestamp"`
}

type Snapshot struct {
	Type      string   `json:"type"`
	Content   string   `json:"content"`
	ClientID  string   `json:"id"`
	Users     []Client `json:"users"`
	Timestamp int64    `json:"timestamp"`
	Seq       uint64   `json:"seq,omitempty"` // correlation id

}
type Presence struct {
	Type       string  `json:"type"`
	ClientID   string  `json:"client_id"`
	Username   string  `json:"username"`
	Color      string  `json:"color"`
	LineNumber float64 `json:"lineNumber"`
	Column     float64 `json:"column"`
}

// Client represents a connected client in a room
type Client struct {
	ID       string          `json:"-"`
	ClientID string          `json:"id"`
	Username string          `json:"username"`
	Conn     *websocket.Conn `json:"-"`
	Room     *Room           `json:"-"`
	Send     chan []byte     `json:"-"`
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// Room represents a collaborative editing session
type Room struct {
	ID         string             `json:"id"`
	Document   *db.Document       `json:"document"`
	Clients    map[string]*Client `json:"clients"`
	Broadcast  chan []byte        `json:"-"`
	Register   chan *Client       `json:"-"`
	Unregister chan *Client       `json:"-"`
	mutex      sync.RWMutex
}

// RoomManager manages all rooms
type RoomManager struct {
	rooms map[string]*Room
	mutex sync.RWMutex
	Store db.PostgresDocumentStore
}

// NewRoomManager creates a new room manager
func NewRoomManager(store db.PostgresDocumentStore) *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*Room),
		Store: store,
	}
}

type Ack struct {
	Type      string `json:"type"`  // "ack"
	Event     string `json:"event"` // "snapshot"
	Seq       uint64 `json:"seq,omitempty"`
	Timestamp int64  `json:"ts"`
}

func (r *Room) SendAck(c *Client, ack Ack, sendClientID string) {
	data, _ := json.Marshal(ack)

	r.mutex.RLock()
	for _, client := range r.Clients {
		if client.ID == sendClientID {
			select {
			case c.Send <- data:
			default:
				// drop on slow client
			}
			break
		}
	}
	r.mutex.RUnlock()
}

// GetOrCreateRoom gets an existing room or creates a new one
func (rm *RoomManager) GetOrCreateRoom(roomID string) (*Room, error) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	room, ok := rm.rooms[roomID]
	if ok {
		return room, nil
	}

	//get document
	document, err := rm.Store.GetDocument(roomID)
	if err != nil {
		return nil, err
	}

	// Create a new room
	room = &Room{
		ID:         roomID,
		Document:   document,
		Clients:    make(map[string]*Client),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Broadcast:  make(chan []byte, 256),
	}

	rm.rooms[roomID] = room

	// Start room.Run() immediately in a goroutine
	go room.run()

	return room, nil
}

// run handles room operations
func (r *Room) run() {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("panic in room.Run: %v\n%s", rec, debug.Stack())
		}
	}()
	log.Println("Room run started")
	for {
		select {
		case client := <-r.Register:
			log.Println("Registering client", client.ID)
			r.mutex.Lock()
			r.Clients[client.ID] = client
			r.mutex.Unlock()
			//send snapshot
			r.sendSnapshot(client)
			// //broadcast user joined
			r.broadcastUserJoined(client)
			log.Printf("Client %s joined room %s", client.ID, r.ID)

		case client := <-r.Unregister:
			log.Println("Unregistering client", client.ID)
			r.mutex.Lock()
			if _, ok := r.Clients[client.ID]; ok {
				delete(r.Clients, client.ID)
				close(client.Send)
			}
			r.mutex.Unlock()

			// Notify other clients about user leaving
			r.broadcastUserLeft(client)
			log.Printf("Client %s left room %s", client.ID, r.ID)

		case message := <-r.Broadcast:
			log.Println("Broadcasting message", string(message))
			r.mutex.RLock()
			for _, client := range r.Clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(r.Clients, client.ID)
				}
			}
			r.mutex.RUnlock()
		}

		log.Println("Clients:", len(r.Clients))
	}

}

// broadcastUserJoined notifies clients about a new user
func (r *Room) broadcastUserJoined(client *Client) {
	message := map[string]interface{}{
		"type":     "user_joined",
		"id":       client.ClientID,
		"username": client.Username,
	}

	data, _ := json.Marshal(message)
	r.Broadcast <- data
}

func (r *Room) BroadcastUserConnected(user *User) {
	message := map[string]interface{}{
		"type":     "init_ok",
		"id":       user.ID,
		"username": user.Username,
	}

	data, _ := json.Marshal(message)
	r.Broadcast <- data
}

func (r *Room) sendSnapshot(c *Client) {
	// Send initial snapshot
	snapshot := map[string]interface{}{
		"type":     "snapshot",
		"id":       c.Room.Document.ID,
		"content":  c.Room.Document.Content,
		"title":    c.Room.Document.Title,
		"language": c.Room.Document.Language,
		"users":    c.Room.GetUsers(),
	}
	msg, _ := json.Marshal(snapshot)
	c.Send <- msg
}

// broadcastUserLeft notifies clients about a user leaving
func (r *Room) broadcastUserLeft(client *Client) {
	message := map[string]interface{}{
		"type":     "user_left",
		"id":       client.ClientID,
		"username": client.Username,
	}

	data, _ := json.Marshal(message)
	r.Broadcast <- data
}

// BroadcastOperation broadcasts an operation to all clients except the sender
func (r *Room) BroadcastOperation(operation *Operation, excludeClientID string) {
	message := map[string]interface{}{
		"type":      "operation",
		"operation": operation,
	}

	data, _ := json.Marshal(message)

	r.mutex.RLock()
	for _, client := range r.Clients {
		if client.ID != excludeClientID {
			select {
			case client.Send <- data:
			default:
				close(client.Send)
				delete(r.Clients, client.ID)
			}
		}
	}
	r.mutex.RUnlock()
}

func (r *Room) BroadcastPresence(presence *Presence, excludeClientID string) {
	message := map[string]interface{}{
		"type":       "presence_user",
		"id":         presence.ClientID,
		"username":   presence.Username,
		"color":      presence.Color,
		"lineNumber": presence.LineNumber,
		"column":     presence.Column,
	}

	data, _ := json.Marshal(message)
	log.Printf("broadcasting presence")

	r.mutex.RLock()
	for _, client := range r.Clients {
		if client.ID != excludeClientID {
			select {
			case client.Send <- data:
			default:
				close(client.Send)
				delete(r.Clients, client.ID)
			}
		}
	}
	r.mutex.RUnlock()

}

// BroadcastOperation broadcasts an operation to all clients except the sender
func (r *Room) BroadcastMetadataUpdate(update *MetadataUpdate, excludeClientID string) {
	message := map[string]interface{}{
		"type":            "document_update",
		"document_update": update,
	}

	data, _ := json.Marshal(message)

	r.mutex.RLock()
	for _, client := range r.Clients {
		if client.ID != excludeClientID {
			select {
			case client.Send <- data:
			default:
				close(client.Send)
				delete(r.Clients, client.ID)
			}
		}
	}
	r.mutex.RUnlock()
}

func (r *Room) BroadcastSnapshotUpdate(snapshot *Snapshot, excludeClientID string) {
	message := map[string]interface{}{
		"type": "snapshot",
		//"id":       c.Room.Document.ID,
		"content": snapshot.Content,
		"users":   snapshot.Users,
		// "title":    snapshot.,
		// "language": snapshot.Language,
	}

	data, _ := json.Marshal(message)

	r.mutex.RLock()
	for _, client := range r.Clients {
		if client.ID != excludeClientID {
			select {
			case client.Send <- data:
			default:
				close(client.Send)
				delete(r.Clients, client.ID)
			}
		}
	}
	r.mutex.RUnlock()
}

// GetUsers returns a list of users currently in the room
func (r *Room) GetUsers() []User {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	users := make([]User, 0, len(r.Clients))
	for _, client := range r.Clients {
		users = append(users, User{
			ID:       client.ClientID,
			Username: client.Username,
		})
	}

	return users
}
