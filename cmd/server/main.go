package main

import (
	"log"

	"github.com/kevo-1/FileToVideo/internal/api"
)

func main() {
	server := api.NewServer(":8080")

	if err := server.Run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
