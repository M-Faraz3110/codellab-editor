package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"collab-editor/pkg/db"
	"collab-editor/pkg/room"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// Handlers contains all HTTP and WebSocket handlers
type Handlers struct {
	roomManager *room.RoomManager
}

// NewHandlers creates a new handlers instance
func NewHandlers(roomManager *room.RoomManager) *Handlers {
	return &Handlers{
		roomManager: roomManager,
	}
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// HandleWebSocket handles WebSocket connections for real-time collaboration
func (h *Handlers) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	vars := mux.Vars(r) //this is lowkey goated
	roomID := vars["roomId"]

	var clientId, username string

	// if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
	// 	// header may contain comma-separated protocols; take first
	// 	id := strings.Split(proto, ",")[0]
	// 	userID = id
	// 	uname := strings.Split(proto, ",")[1]
	// 	username = uname

	// }

	// Get or create room
	roomInstance, err := h.roomManager.GetOrCreateRoom(roomID)
	if err != nil {
		log.Printf("Error getting room %s: %v", roomID, err)
		conn.Close()
		return
	}

	// Create client
	client := &room.Client{
		ID:       uuid.New().String(),
		ClientID: clientId,
		Username: username,
		Conn:     conn,
		Room:     roomInstance,
		Send:     make(chan []byte, 256),
	}

	// Start goroutines for reading and writing
	go h.writePump(client)
	go h.readPump(client)

	// Register client with room
	roomInstance.Register <- client

}

// readPump handles reading messages from the WebSocket
func (h *Handlers) readPump(c *room.Client) {
	log.Println("Starting readPump for", c.ID)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in readPump for %s: %v\n%s", c.ID, r, debug.Stack())
		}
		// signal the room to unregister this client (non-blocking attempt)
		select {
		case c.Room.Unregister <- c:
		default:
		}
		// close connection only here — do NOT close c.Send here
		log.Println("readPump closing for", c.ID)
		c.Conn.Close()
		log.Println("readPump exiting for", c.ID)
	}()

	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		log.Println("About to read message for", c.ID)
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			log.Printf("ReadMessage error for %s: %v", c.ID, err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket unexpected close for %s: %v", c.ID, err)
			}
			break
		}
		log.Println("message: " + string(message))

		// Parse message
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Error parsing message from %s: %v", c.ID, err)
			continue
		}

		switch msg["type"] {
		case "init":
			// Only broadcast user_joined when the client sends explicit init/ready
			// if u, ok := msg["username"].(string); ok && u != "" {
			// 	c.Username = u
			// }
			h.handleInit(c, msg)
			//c.Room.Broadcast <- userJoinedMsg
		case "operation":
			log.Printf("received operation")
			h.handleOperation(c, msg)
		case "ping":
			// application-level ping -> send a pong via Send channel
			c.Send <- []byte(`{"type":"pong"}`)
		case "document_update":
			h.handleDocUpdate(c, msg)
		case "snapshot":
			var snapshot room.Snapshot
			err := json.Unmarshal(message, &snapshot)
			if err != nil {
				log.Printf("error parsing snapshot: %v", err)
				continue
			}
			h.handleSnapshot(c, snapshot)
		case "presence_user":
			log.Printf("received presence update")
			h.handlePresence(c, msg)
		default:
			log.Printf("Unknown message type from %s: %v", c.ID, msg["type"])
		}
	}
}

// writePump handles writing messages to the WebSocket
func (h *Handlers) writePump(c *room.Client) {
	log.Println("Starting writePump for", c.ID)
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		// Ensure the client is unregistered and that the connection is closed
		select {
		case c.Room.Unregister <- c:
		default:
		}
		log.Println("writePump closing for", c.ID)
		c.Conn.Close()
		log.Println("Exiting writePump for", c.ID)
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// channel closed: send close and return
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			log.Println("writing message" + string(message))
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("WebSocket write error for %s: %v", c.ID, err)
				// signal the room to unregister this client (non-blocking)
				select {
				case c.Room.Unregister <- c:
				default:
				}
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Ping error for %s: %v", c.ID, err)
				select {
				case c.Room.Unregister <- c:
				default:
				}
				return
			}
		}
	}
}

// handleOperation processes text operations from clients
func (h *Handlers) handleOperation(client *room.Client, msg map[string]interface{}) {
	operationData, ok := msg["operation"].(map[string]interface{})
	if !ok {
		log.Printf("Invalid operation format")
		return
	}

	operation := &room.Operation{
		Type:      operationData["type"].(string),
		Position:  int(operationData["position"].(float64)),
		Content:   operationData["content"].(string),
		Length:    int(operationData["length"].(float64)),
		ClientID:  client.ID,
		Timestamp: time.Now().UnixNano(),
	}

	// Broadcast operation to other clients
	client.Room.BroadcastOperation(operation, client.ID)

	// Update document content (simplified - in production, use operational transformation)
	h.updateDocumentContent(client.Room, operation)
}

func (h *Handlers) handleInit(client *room.Client, msg map[string]interface{}) {
	id, ok1 := msg["id"].(string)
	username, ok2 := msg["username"].(string)

	if !ok1 || !ok2 {
		log.Printf("Invalid init format")
	}

	client.ClientID = id
	client.Username = username

	initok := &room.User{
		ID:       id,
		Username: username,
	}

	client.Room.BroadcastUserConnected(initok)
}

func (h *Handlers) handleDocUpdate(client *room.Client, msg map[string]interface{}) {
	title, okt := msg["title"].(string)
	language, okl := msg["language"].(string)

	if !okt || !okl {
		log.Printf("Invalid update format")
		return
	}

	update := &room.MetadataUpdate{
		Type:      "document_update",
		Title:     title,
		Language:  language,
		ClientID:  client.ID,
		Timestamp: time.Now().UnixNano(),
	}

	client.Room.Document.Title = title
	client.Room.Document.Language = language

	client.Room.BroadcastMetadataUpdate(update, client.ID)

	h.updateDocumentMetadata(client.Room, update)
}

func (h *Handlers) handleSnapshot(client *room.Client, msg room.Snapshot) {
	users := make([]room.Client, len(msg.Users))
	for i, user := range msg.Users {
		users[i] = room.Client{
			ID:       user.ID,
			ClientID: user.ClientID,
			Username: user.Username,
		}
	}

	//so inconsistent...
	snapshot := &room.Snapshot{
		Type:      "snapshot",
		Content:   msg.Content,
		ClientID:  client.ClientID,
		Users:     users,
		Timestamp: time.Now().UnixNano(),
	}

	client.Room.Document.Content = msg.Content

	h.updateDocumentSnapshot(client.Room, snapshot)

	client.Room.BroadcastSnapshotUpdate(snapshot, client.ID)

	client.Room.SendAck(client, room.Ack{
		Type:      "ack",
		Event:     "snapshot",
		Seq:       msg.Seq,
		Timestamp: time.Now().UnixNano(),
	}, client.ID)
}

func (h *Handlers) handlePresence(client *room.Client, msg map[string]interface{}) {
	username, oku := msg["username"].(string)
	color, okc := msg["color"].(string)
	lineNumber, okl := msg["lineNumber"].(float64)
	column, okcl := msg["column"].(float64)

	if !oku || !okc || !okl || !okcl {
		log.Printf("Invalid presence format")
		return
	}

	presence := &room.Presence{
		Type:       "presence_user",
		ClientID:   client.ClientID,
		Username:   username,
		Color:      color,
		LineNumber: lineNumber,
		Column:     column,
	}

	client.Room.BroadcastPresence(presence, client.ID) //dont need to persist this

}

// updateDocumentContent updates the document content based on the operation
// This is the ONLY way to update document content in the collaborative editor
func (h *Handlers) updateDocumentContent(room *room.Room, operation *room.Operation) {
	// This is a simplified implementation
	// In production, you would use operational transformation algorithms
	// to handle concurrent edits properly

	switch operation.Type {
	case "insert":
		// Insert text at position
		content := room.Document.Content
		if operation.Position >= len(content) {
			room.Document.Content = content + operation.Content
		} else {
			room.Document.Content = content[:operation.Position] + operation.Content + content[operation.Position:]
		}
	case "delete":
		// Delete text at position
		content := room.Document.Content
		if operation.Position+operation.Length <= len(content) {
			room.Document.Content = content[:operation.Position] + content[operation.Position+operation.Length:]
		}
	}

	// Update version
	room.Document.Version++

	// we dont commit this to db, just broadcast it to reduce load
}

func (h *Handlers) updateDocumentMetadata(room *room.Room, update *room.MetadataUpdate) {
	room.Document.Version++
	updates := db.DocumentUpdate{
		Title:    &update.Title,
		Content:  &room.Document.Content,
		Language: &update.Language,
	}

	_, err := h.roomManager.Store.UpdateDocument(room.ID, &updates)
	if err != nil {
		log.Printf("failed to updated doc")
		return
	}
}

func (h *Handlers) updateDocumentSnapshot(room *room.Room, snapshot *room.Snapshot) {
	room.Document.Version++
	updates := db.DocumentUpdate{
		Content: &snapshot.Content,
	}

	_, err := h.roomManager.Store.UpdateDocument(room.ID, &updates)
	if err != nil {
		log.Printf("failed to updated doc")
		return
	}
}

// CreateDocument creates a new document
func (h *Handlers) CreateDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    string `json:"title"`
		Content  string `json:"content"`
		Language string `json:"language"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	doc, err := h.roomManager.Store.CreateDocument(req.Title, req.Content, req.Language)
	if err != nil {
		http.Error(w, "Failed to create document", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

// ListDocuments returns a list of documents
func (h *Handlers) ListDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := h.roomManager.Store.ListDocuments()
	if err != nil {
		http.Error(w, "Failed to list documents", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(docs)
}

// GetDocument retrieves a document by ID
func (h *Handlers) GetDocument(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	doc, err := h.roomManager.Store.GetDocument(id)
	if err != nil {
		http.Error(w, "Document not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

// DeleteDocument deletes a document
func (h *Handlers) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	err := h.roomManager.Store.DeleteDocument(id)
	if err != nil {
		http.Error(w, "Failed to delete document", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetRoomUsers returns the list of users in a room
func (h *Handlers) GetRoomUsers(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	roomID := vars["roomId"]

	room, err := h.roomManager.GetOrCreateRoom(roomID)
	if err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	users := room.GetUsers()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"room_id": roomID,
		"users":   users,
	})
}
