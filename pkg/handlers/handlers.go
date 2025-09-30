package handlers

import (
	"encoding/json"
	"log"
	"net/http"
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
	defer conn.Close()

	vars := mux.Vars(r) //this is lowkey goated
	roomID := vars["roomId"]

	// Get username from query parameter
	username := r.URL.Query().Get("username")
	if username == "" {
		username = "Anonymous"
	}

	// Get or create room
	roomInstance, err := h.roomManager.GetOrCreateRoom(roomID)
	if err != nil {
		log.Printf("Error getting room %s: %v", roomID, err)
		return
	}

	// Create client
	client := &room.Client{
		ID:       uuid.New().String(),
		Username: username,
		Conn:     conn,
		Room:     roomInstance,
		Send:     make(chan []byte, 256),
	}

	// Register client with room
	roomInstance.Register <- client

	// Start goroutines for reading and writing
	go h.writePump(client)
	go h.readPump(client)
}

// readPump handles reading messages from the WebSocket
func (h *Handlers) readPump(c *room.Client) {
	log.Println("Starting readPump for", c.ID)
	defer func() {
		if r := recover(); r != nil {
			log.Println("Recovered in readPump", r)
		}
		log.Println("readPump exiting for", c.ID)
		c.Room.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		log.Println("About to read message")
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			log.Printf("ReadMessage error: %v", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Parse the message
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Error parsing message: %v", err)
			continue
		}

		// Handle different message types
		switch msg["type"] {
		case "operation":
			h.handleOperation(c, msg)
		case "ping":
			// Respond to ping with pong
			//c.Conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
			c.Send <- []byte(`{"type":"pong"}`)
		}
	}
}

// writePump handles writing messages to the WebSocket
func (h *Handlers) writePump(c *room.Client) {
	log.Println("Starting writePump for", c.ID)
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		log.Println("Exiting writePump for", c.ID)
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
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

	updates := db.DocumentUpdate{
		Title:    nil,
		Content:  &room.Document.Content,
		Language: nil,
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
		Title   string `json:"title"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	doc, err := h.roomManager.Store.CreateDocument(req.Title, req.Content)
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
