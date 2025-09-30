package main

import (
	"log"

	"collab-editor/app"
)

func main() {
	server := app.NewServer()
	log.Fatal(server.Start(""))
}
