package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/kevo-1/FileToVideo/internal/infrastructure"
	"github.com/kevo-1/FileToVideo/internal/repository"
)

// Handles up to 10MB
const MaxUploadSizeBytes = 10 << 20

// Start by parsing the file from the request
// Then log it's metadata, and create a tempfile for processing later on
func HandleUpload(w http.ResponseWriter, r *http.Request, tmpFileRepo *repository.TempFileRepo, fileQueue *infrastructure.FileQueue) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSizeBytes)

	if err := r.ParseMultipartForm(MaxUploadSizeBytes); err != nil {
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

	tmpFile, err := tmpFileRepo.CreateTempFile(handler.Filename, reqId)
	if err != nil {
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(tmpFile, file); err != nil {
		log.Printf("error writing temp file for reqId=%s: %v", reqId, err)
		if delErr := tmpFileRepo.DeleteTempFile(reqId); delErr != nil {
			log.Printf("failed to delete temp file after write error: %v", delErr)
		}
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}

	if err := tmpFile.Close(); err != nil {
		log.Printf("error closing temp file for reqId=%s: %v", reqId, err)
		if delErr := tmpFileRepo.DeleteTempFile(reqId); delErr != nil {
			log.Printf("failed to delete temp file after close error: %v", delErr)
		}
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}

	fileQueue.EnqueueTask(tmpFile)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{"requestId": reqId}); err != nil {
		log.Printf("error encoding response for reqId=%s: %v", reqId, err)
	}
}
