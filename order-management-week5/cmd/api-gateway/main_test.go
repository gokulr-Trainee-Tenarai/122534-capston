package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGraphiQLPageIsServed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/graphiql", serveGraphiQL)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/graphiql", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("GET /graphiql returned %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want HTML", contentType)
	}
	for _, expected := range []string{
		"GraphiQL",
		"/graphql",
		"JWT access token",
		"headers.Authorization",
		"orders(limit: 10, offset: 0)",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("GraphiQL page does not contain %q", expected)
		}
	}
}
