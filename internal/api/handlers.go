package api

import (
	"fmt"
	"net/http"
)

func HandleUpload(w http.ResponseWriter, r *http.Request) {
	fmt.Println("uploaded")
}
