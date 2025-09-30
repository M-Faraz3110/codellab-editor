package db

// createTable creates the documents table if it doesn't exist
func (s *PostgresDocumentStore) createTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS documents (
		id VARCHAR(36) PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		content TEXT NOT NULL,
		language TEXT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE NOT NULL,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
		version INTEGER NOT NULL DEFAULT 1
	);
	
	CREATE INDEX IF NOT EXISTS idx_documents_created_at ON documents(created_at);
	CREATE INDEX IF NOT EXISTS idx_documents_updated_at ON documents(updated_at);
	`

	_, err := s.db.Exec(query)
	return err
}
