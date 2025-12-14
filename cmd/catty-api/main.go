package main

import (
	"log"
	"os"

	"github.com/izalutski/catty/internal/api"
)

func main() {
	addr := os.Getenv("CATTY_API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:4815"
	}

	server, err := api.NewServer(addr)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	if err := server.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
