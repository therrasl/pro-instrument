package middleware

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *responseWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func RequestLogger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			startedAt := time.Now()
			writer := &responseWriter{ResponseWriter: response, status: http.StatusOK}

			next.ServeHTTP(writer, request)
			logger.Printf("method=%s path=%s status=%d duration=%s", request.Method, request.URL.Path, writer.status, time.Since(startedAt))
		})
	}
}

func Recovery(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Printf("panic recovered: %v", recovered)
					response.Header().Set("Content-Type", "application/json")
					response.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(response).Encode(map[string]string{"error": "internal server error"})
				}
			}()

			next.ServeHTTP(response, request)
		})
	}
}
