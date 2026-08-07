package api

import (
	"log"
	"net/http"
)

type Server struct {
	port string
}

func NewServer(port string) *Server {
	return &Server{port: port}
}

func (s *Server) Run() error {

	router := http.NewServeMux()
	router.HandleFunc("POST /upload", HandleUpload)

	log.Printf("Starting server on port %s\n", s.port)
	return http.ListenAndServe(s.port, router)
}
