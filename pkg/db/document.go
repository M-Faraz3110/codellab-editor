package db

import "time"

// Document represents a document in the collaborative editor
type Document struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Language  string    `json:"language,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int       `json:"version"`
}

// DocumentStore interface for document persistence
type IDocumentStore interface {
	CreateDocument(title, content string) (*Document, error)
	GetDocument(id string) (*Document, error)
	// UpdateDocument applies partial updates. Use pointer fields in DocumentUpdate
	// to indicate which fields should be modified.
	UpdateDocument(id string, updates *DocumentUpdate) (*Document, error)
	DeleteDocument(id string) error
	ListDocuments() ([]*Document, error)
}

// DocumentUpdate represents partial updates to a document. Pointer fields
// allow distinguishing between "not provided" (nil) and "set to empty".
type DocumentUpdate struct {
	Title    *string `json:"title,omitempty"`
	Content  *string `json:"content,omitempty"`
	Language *string `json:"language,omitempty"`
}
