package main

import (
	"github.com/kevo-1/FileToVideo/internal/api"
)

func main() {
	server := api.NewServer(":8080")

	server.Run()
}
