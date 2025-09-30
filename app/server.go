package app

import (
	"log"
	"net/http"

	"collab-editor/pkg/config"
	"collab-editor/pkg/db"
	"collab-editor/pkg/handlers"
	"collab-editor/pkg/room"

	"github.com/gorilla/mux"
)

// Server represents the application server
type Server struct {
	router      *mux.Router
	roomManager *room.RoomManager
	handlers    *handlers.Handlers
	docStore    db.IDocumentStore
	config      *config.Config
}

// NewServer creates a new server instance
func NewServer() *Server {
	// Load configuration
	cfg := config.Load()

	// Initialize PostgreSQL storage
	docStore, err := db.NewPostgresDocumentStore(cfg.GetDatabaseConnectionString())
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	roomManager := room.NewRoomManager(*docStore)

	// Initialize handlers
	h := handlers.NewHandlers(roomManager)

	// Setup routes
	r := mux.NewRouter()

	// WebSocket endpoint for real-time collaboration
	r.HandleFunc("/ws/{roomId}", h.HandleWebSocket)

	// REST API endpoints (read-only for documents)
	r.HandleFunc("/api/documents", h.CreateDocument).Methods("POST")
	r.HandleFunc("/api/documents", h.ListDocuments).Methods("GET")
	r.HandleFunc("/api/documents/{id}", h.GetDocument).Methods("GET")
	r.HandleFunc("/api/documents/{id}", h.DeleteDocument).Methods("DELETE")
	r.HandleFunc("/api/rooms/{roomId}/users", h.GetRoomUsers).Methods("GET")

	// CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	return &Server{
		router:      r,
		roomManager: roomManager,
		handlers:    h,
		docStore:    docStore,
		config:      cfg,
	}
}

// Start starts the server
func (s *Server) Start(addr string) error {
	if addr == "" {
		addr = s.config.GetServerAddr()
	}
	log.Printf("Starting collaborative editor server on %s", addr)
	// Wrap the router with a top-level CORS middleware so that
	// preflight (OPTIONS) requests are handled before mux does
	// method-based matching (which can otherwise return 405).
	return http.ListenAndServe(addr, corsMiddleware(s.router))
}

// corsMiddleware handles CORS headers and responds to preflight requests
// at the outer layer so they don't get rejected by method-restricted routes.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Debug: log preflight requests so we can confirm middleware runs
		if r.Method == http.MethodOptions {
			log.Printf("CORS preflight received: %s %s Origin=%s ReqHeaders=%s", r.Method, r.URL.Path, r.Header.Get("Origin"), r.Header.Get("Access-Control-Request-Headers"))
		}
		origin := r.Header.Get("Origin")
		if origin != "" {
			// Reflect the origin for stricter CORS (avoids some browser issues with credentials)
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		// Always advertise the allowed methods
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		// If the browser asked for specific headers, echo them back; otherwise allow common headers
		if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
			w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
		} else {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		// Allow caching preflight for a short duration
		w.Header().Set("Access-Control-Max-Age", "600")

		// Inform caches that response varies by Origin and Access-Control-Request-Headers
		w.Header().Add("Vary", "Origin")
		w.Header().Add("Vary", "Access-Control-Request-Headers")

		if r.Method == http.MethodOptions {
			// Respond to preflight immediately
			w.WriteHeader(http.StatusNoContent) // 204
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Close closes the server and database connections
func (s *Server) Close() error {
	if postgresStore, ok := s.docStore.(*db.PostgresDocumentStore); ok {
		return postgresStore.Close()
	}
	return nil
}
