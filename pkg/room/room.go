package room

import (
	"encoding/json"
	"log"
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

// Client represents a connected client in a room
type Client struct {
	ID       string          `json:"id"`
	Username string          `json:"username"`
	Conn     *websocket.Conn `json:"-"`
	Room     *Room           `json:"-"`
	Send     chan []byte     `json:"-"`
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

// GetOrCreateRoom gets an existing room or creates a new one
func (rm *RoomManager) GetOrCreateRoom(roomID string) (*Room, error) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	room, exists := rm.rooms[roomID]
	if !exists {
		// Try to get document from storage
		doc, err := rm.Store.GetDocument(roomID)
		if err != nil {
			// Create a new document if it doesn't exist
			doc, err = rm.Store.CreateDocument("Untitled Document", "")
			if err != nil {
				return nil, err
			}
		}

		room = &Room{
			ID:         roomID,
			Document:   doc,
			Clients:    make(map[string]*Client),
			Broadcast:  make(chan []byte, 256),
			Register:   make(chan *Client, 10),
			Unregister: make(chan *Client, 10),
		}

		rm.rooms[roomID] = room
		go room.run()
	}

	return room, nil
}

// run handles room operations
func (r *Room) run() {
	log.Println("Room run started")
	for {
		select {
		case client := <-r.Register:
			log.Println("Registering client", client.ID)
			r.mutex.Lock()
			r.Clients[client.ID] = client
			r.mutex.Unlock()

			// Notify other clients about new user
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
		"type": "user_joined",
		"user": map[string]interface{}{
			"id":       client.ID,
			"username": client.Username,
		},
	}

	data, _ := json.Marshal(message)
	r.Broadcast <- data
}

// broadcastUserLeft notifies clients about a user leaving
func (r *Room) broadcastUserLeft(client *Client) {
	message := map[string]interface{}{
		"type": "user_left",
		"user": map[string]interface{}{
			"id":       client.ID,
			"username": client.Username,
		},
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

// GetUsers returns a list of users currently in the room
func (r *Room) GetUsers() []map[string]interface{} {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	users := make([]map[string]interface{}, 0, len(r.Clients))
	for _, client := range r.Clients {
		users = append(users, map[string]interface{}{
			"id":       client.ID,
			"username": client.Username,
		})
	}

	return users
}
