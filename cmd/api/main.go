// entrypoint 


package main

import (
	"log"
	"net/http"

	"iossa/internal/handler"
	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()

	r.Post("/analyze", handler.AnalyzeHandler)

	log.Println("Server running on :8080")
	http.ListenAndServe(":8080", r)
}