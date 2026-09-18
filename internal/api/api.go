package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/kevo-1/FileToVideo/internal/infrastructure"
	"github.com/kevo-1/FileToVideo/internal/repository"
)

type Server struct {
	addr string
}

func NewServer(addr string) *Server {
	return &Server{addr: addr}
}

const (
	WorkerCount     = 10
	QueueBufferSize = 100
)

func (s *Server) Run() error {
	tmpFileRepo, err := repository.NewTempFileRepo()
	if err != nil {
		return err
	}

	fileQueue := infrastructure.NewFileQueue(WorkerCount, QueueBufferSize)

	router := http.NewServeMux()
	router.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		HandleUpload(w, r, tmpFileRepo, fileQueue)
	})

	httpServer := &http.Server{
		Addr:    s.addr,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErrCh := make(chan error, 1)
	go func() {
		log.Printf("Starting server on %s\n", s.addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
			return
		}
		serveErrCh <- nil
	}()

	select {
	case err := <-serveErrCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		log.Println("Shutdown signal received!")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("error during server shutdown: %v", err)
		}
		fileQueue.Wait()
		if err := tmpFileRepo.Close(); err != nil {
			log.Printf("error closing temp file repo: %v", err)
		}
	}

	return nil
}
