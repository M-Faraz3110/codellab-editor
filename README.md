## TO-DO
1. Auth
2. Index
   

# Collaborative Code Editor

A real-time collaborative code editor built with Go backend and WebSocket communication.

## Features

- **Real-time Collaboration**: Multiple users can edit the same document simultaneously
- **WebSocket Communication**: Low-latency real-time updates
- **Document Management**: Create, read, update, and delete documents
- **User Sessions**: Track active users in each room
- **RESTful API**: HTTP endpoints for document management

## Architecture

### Backend (Go)
- **WebSocket Server**: Handles real-time communication
- **Room Management**: Manages collaborative editing sessions
- **Document Storage**: In-memory document persistence
- **Operation Broadcasting**: Distributes text operations to all clients

### Key Components

1. **Room Manager**: Manages collaborative editing rooms
2. **Document Store**: Handles document persistence
3. **WebSocket Handler**: Manages real-time connections
4. **Operation System**: Handles text operations (insert, delete, retain)

## Project Structure

```
collab-editor/
├── main.go                 # Server entry point
├── go.mod                  # Go dependencies
├── .env.example            # Configuration template
├── app/
│   └── server.go          # Application server setup
├── pkg/
│   ├── config/            # Configuration management
│   ├── handlers/          # HTTP & WebSocket handlers
│   ├── room/              # Room management & operations
│   └── db/                # Document persistence
├── scripts/
│   └── setup-db.sh        # Database setup script
└── test_client.html       # Demo client
```

## API Endpoints

### WebSocket
- `WS /ws/{roomId}?username={username}` - Connect to a collaborative room

### REST API
- `POST /api/documents` - Create a new document
- `GET /api/documents/{id}` - Get a document by ID (read-only)
- `DELETE /api/documents/{id}` - Delete a document
- `GET /api/rooms/{roomId}/users` - Get users in a room

**Note**: Document content can only be updated via WebSocket operations for real-time collaboration.

## Configuration

The application supports configuration through environment variables or a `.env` file. Copy `.env.example` to `.env` and modify as needed.

### Available Configuration Options:

| Variable      | Default         | Description                      |
| ------------- | --------------- | -------------------------------- |
| `SERVER_HOST` | `localhost`     | Server host address              |
| `SERVER_PORT` | `8080`          | Server port                      |
| `DB_HOST`     | `localhost`     | PostgreSQL host                  |
| `DB_PORT`     | `5432`          | PostgreSQL port                  |
| `DB_USER`     | `postgres`      | PostgreSQL username              |
| `DB_PASSWORD` | `postgres`      | PostgreSQL password              |
| `DB_NAME`     | `collab_editor` | Database name                    |
| `DB_SSLMODE`  | `disable`       | SSL mode for database connection |

## Getting Started

1. **Install Dependencies**:
   ```bash
   go mod tidy
   ```

2. **Setup PostgreSQL Database**:
   ```bash
   # Install PostgreSQL (if not already installed)
   # macOS: brew install postgresql
   # Ubuntu: sudo apt-get install postgresql postgresql-contrib
   
   # Create database
   createdb collab_editor
   ```

3. **Configure the Application** (optional - defaults shown):
   
   **Option A: Using .env file** (recommended for development):
   ```bash
   cp .env.example .env
   # Edit .env file with your configuration
   ```
   
   **Option B: Using environment variables**:
   ```bash
   export SERVER_HOST=localhost
   export SERVER_PORT=8080
   export DB_HOST=localhost
   export DB_PORT=5432
   export DB_USER=postgres
   export DB_PASSWORD=postgres
   export DB_NAME=collab_editor
   export DB_SSLMODE=disable
   ```

4. **Run the Server**:
   ```bash
   go run main.go
   ```

5. **Connect via WebSocket**:
   ```javascript
   const ws = new WebSocket('ws://localhost:8080/ws/room123?username=YourName');
   ```

## WebSocket Message Format

### Client to Server
```json
{
  "type": "operation",
  "operation": {
    "type": "insert",
    "position": 10,
    "content": "Hello",
    "length": 0,
    "client_id": "client123",
    "timestamp": 1234567890
  }
}
```

### Server to Client
```json
{
  "type": "operation",
  "operation": {
    "type": "insert",
    "position": 10,
    "content": "Hello",
    "length": 0,
    "client_id": "client123",
    "timestamp": 1234567890
  }
}
```

## Development Notes

- **PostgreSQL storage** - Documents are persisted to database
- Basic operational transformation (simplified for demo)
- CORS enabled for development
- No authentication/authorization (for demo purposes)
- **Document updates are WebSocket-only** to prevent data loss in collaborative editing

## Next Steps

- [ ] Implement proper operational transformation
- [x] Add persistent storage (database) ✅
- [ ] Add user authentication
- [ ] Implement conflict resolution
- [ ] Add frontend client
- [ ] Add syntax highlighting
- [ ] Add file management
