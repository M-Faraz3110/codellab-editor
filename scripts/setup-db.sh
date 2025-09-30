#!/bin/bash

# Database setup script for collaborative editor
# This script creates the PostgreSQL database and user

set -e

# Default values
DB_NAME=${DB_NAME:-collab_editor}
DB_USER=${DB_USER:-postgres}
DB_PASSWORD=${DB_PASSWORD:-postgres}
DB_HOST=${DB_HOST:-localhost}
DB_PORT=${DB_PORT:-5432}

echo "Setting up PostgreSQL database for collaborative editor..."
echo "Database: $DB_NAME"
echo "User: $DB_USER"
echo "Host: $DB_HOST:$DB_PORT"

# Check if PostgreSQL is running
if ! pg_isready -h $DB_HOST -p $DB_PORT -U $DB_USER; then
    echo "Error: PostgreSQL is not running or not accessible at $DB_HOST:$DB_PORT"
    echo "Please start PostgreSQL and try again."
    exit 1
fi

# Create database if it doesn't exist
echo "Creating database '$DB_NAME'..."
createdb -h $DB_HOST -p $DB_PORT -U $DB_USER $DB_NAME 2>/dev/null || echo "Database '$DB_NAME' already exists or creation failed."

echo "Database setup complete!"
echo ""
echo "You can now start the server with:"
echo "  go run main.go"
echo ""
echo "Environment variables (optional):"
echo "  export DB_HOST=$DB_HOST"
echo "  export DB_PORT=$DB_PORT"
echo "  export DB_USER=$DB_USER"
echo "  export DB_PASSWORD=$DB_PASSWORD"
echo "  export DB_NAME=$DB_NAME"
echo "  export DB_SSLMODE=disable"
