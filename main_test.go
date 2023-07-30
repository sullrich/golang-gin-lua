package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func BenchmarkRunLuaScript(b *testing.B) {
	router := gin.New()

	// setup the same middlewares as in main()
	// ...

	router.POST("/runLuaFile/:filename", func(c *gin.Context) {
		// the same code as in main()
		// ...
	})

	jsonPayload := `{
		"name": "Test User"
	}`

	b.ResetTimer() // resets the timer to ignore the setup time

	for i := 0; i < b.N; i++ {
		request, _ := http.NewRequest(http.MethodPost, "/runLuaFile/test.lua", strings.NewReader(jsonPayload))
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)
	}
}
