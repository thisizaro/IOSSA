// entrypoint 


package main

import (
	"log"
	"net/http"
	"os
	"

	"iossa/internal/handler"
	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()

	// Root route
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("IOSSA API is running 🚀"))
	})

	// Your main endpoint
	r.Post("/analyze", handler.AnalyzeHandler)

	// PORT handling (important for Cloud Run)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}


	
	log.Println("Server running on :8080")
	http.ListenAndServe(":8080", r)
}