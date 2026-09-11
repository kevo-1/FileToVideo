package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/kevo-1/FileToVideo/internal/services"
)

// Handles up to 10MB
const MAX_UPLOAD_SIZE_BYTES = 10 << 20

// Start by parsing the file from the request
// Then log it's metadata, and create a tempfile for processing later on
func HandleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MAX_UPLOAD_SIZE_BYTES)

	if err := r.ParseMultipartForm(MAX_UPLOAD_SIZE_BYTES); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}

	defer file.Close()

	log.Printf("Uploaded File: %s\n", handler.Filename)
	log.Printf("File Size: %d\n", handler.Size)

	reqId := uuid.NewString()

	dst, err := services.CreateTempFile(handler.Filename, reqId)
	if err != nil {
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}

	if _, err := dst.ReadFrom(file); err != nil {
		path := dst.Name()
		dst.Close()
		if delErr := services.DeleteTempFile(path); delErr != nil {
			log.Printf("failed to delete temp file after write error: %v", delErr)
		}
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"requestId": reqId})
}
