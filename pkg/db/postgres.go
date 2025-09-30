package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// PostgresDocumentStore implements DocumentStore using PostgreSQL
type PostgresDocumentStore struct {
	db *sql.DB
}

// NewPostgresDocumentStore creates a new PostgreSQL document store
func NewPostgresDocumentStore(connStr string) (*PostgresDocumentStore, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &PostgresDocumentStore{db: db}

	// Create the documents table if it doesn't exist
	if err := store.createTable(); err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	return store, nil
}

// Close closes the database connection
func (s *PostgresDocumentStore) Close() error {
	return s.db.Close()
}

func (s *PostgresDocumentStore) CreateDocument(title, content string) (*Document, error) {
	id := uuid.New().String()
	now := time.Now()

	query := `
		INSERT INTO documents (id, title, content, language, created_at, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, title, content, language, created_at, updated_at, version
	`

	doc := &Document{}
	err := s.db.QueryRow(query, id, title, content, "", now, now, 1).Scan(
		&doc.ID,
		&doc.Title,
		&doc.Content,
		&doc.Language,
		&doc.CreatedAt,
		&doc.UpdatedAt,
		&doc.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create document: %w", err)
	}

	return doc, nil
}

func (s *PostgresDocumentStore) GetDocument(id string) (*Document, error) {
	query := `
		SELECT id, title, content, language, created_at, updated_at, version
		FROM documents
		WHERE id = $1
	`

	doc := &Document{}
	err := s.db.QueryRow(query, id).Scan(
		&doc.ID,
		&doc.Title,
		&doc.Content,
		&doc.Language,
		&doc.CreatedAt,
		&doc.UpdatedAt,
		&doc.Version,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrDocumentNotFound
		}
		return nil, fmt.Errorf("failed to get document: %w", err)
	}

	return doc, nil
}

func (s *PostgresDocumentStore) UpdateDocument(id string, updates *DocumentUpdate) (*Document, error) {
	// Build dynamic SET clauses for provided fields
	sets := []string{}
	args := []interface{}{}
	argPos := 1

	if updates.Title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", argPos))
		args = append(args, *updates.Title)
		argPos++
	}
	if updates.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d", argPos))
		args = append(args, *updates.Content)
		argPos++
	}
	if updates.Language != nil {
		sets = append(sets, fmt.Sprintf("language = $%d", argPos))
		args = append(args, *updates.Language)
		argPos++
	}

	if len(sets) == 0 {
		// Nothing to update; return current document
		return s.GetDocument(id)
	}

	// Always update updated_at and version
	now := time.Now()
	sets = append(sets, fmt.Sprintf("updated_at = $%d", argPos))
	args = append(args, now)
	argPos++
	sets = append(sets, "version = version + 1")

	// Add id param
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE documents
		SET %s
		WHERE id = $%d
		RETURNING id, title, content, language, created_at, updated_at, version
	`, strings.Join(sets, ", "), argPos)

	doc := &Document{}
	err := s.db.QueryRow(query, args...).Scan(
		&doc.ID,
		&doc.Title,
		&doc.Content,
		&doc.Language,
		&doc.CreatedAt,
		&doc.UpdatedAt,
		&doc.Version,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrDocumentNotFound
		}
		return nil, fmt.Errorf("failed to update document: %w", err)
	}

	return doc, nil
}

func (s *PostgresDocumentStore) DeleteDocument(id string) error {
	query := `DELETE FROM documents WHERE id = $1`

	result, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrDocumentNotFound
	}

	return nil
}

func (s *PostgresDocumentStore) ListDocuments() ([]*Document, error) {
	query := `
		SELECT id, title, content, language, created_at, updated_at, version
		FROM documents
		ORDER BY updated_at DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}
	defer rows.Close()

	var documents []*Document
	for rows.Next() {
		doc := &Document{}
		err := rows.Scan(
			&doc.ID,
			&doc.Title,
			&doc.Content,
			&doc.Language,
			&doc.CreatedAt,
			&doc.UpdatedAt,
			&doc.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan document: %w", err)
		}
		documents = append(documents, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	return documents, nil
}

// Compile-time check to ensure PostgresDocumentStore implements DocumentStore interface
// This will cause a compilation error if any interface methods are missing or have wrong signatures
var _ IDocumentStore = (*PostgresDocumentStore)(nil)
