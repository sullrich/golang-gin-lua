package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/yuin/gopher-lua"
	"golang.org/x/time/rate"

	"io/ioutil"
	"path/filepath"
	"sync"
)

// Cache for Lua scripts
var scriptCache = make(map[string]string)
var scriptCacheMutex = &sync.RWMutex{}

// A map of rate limiters for each IP.
var visitors = make(map[string]*rate.Limiter)
var mtx = sync.Mutex{}

// Create a new rate limiter and add it to the visitors map, using the
// IP address as the key.
func addVisitor(ip string) *rate.Limiter {
	limiter := rate.NewLimiter(1, 3)
	mtx.Lock()
	// Include the current time when creating a new visitor.
	visitors[ip] = limiter
	mtx.Unlock()
	return limiter
}

// Retrieve and return the rate limiter for the current visitor if it
// already exists. Otherwise call the addVisitor function to add a
// new entry to the map.
func getVisitor(ip string) *rate.Limiter {
	mtx.Lock()
	limiter, exists := visitors[ip]
	mtx.Unlock()
	if !exists {
		return addVisitor(ip)
	}
	return limiter
}

// Helper function to convert map to Lua table
func mapToTable(L *lua.LState, m map[string]interface{}) *lua.LTable {
	tbl := L.CreateTable(0, len(m))
	for k, v := range m {
		L.SetTable(tbl, lua.LString(k), lua.LString(v.(string))) // modify here if v isn't string
	}
	return tbl
}

func runLuaScript(filename string, jsonData map[string]interface{}) (string, error) {
	// Check if the script is in cache
	scriptCacheMutex.RLock()
	content, ok := scriptCache[filename]
	scriptCacheMutex.RUnlock()

	// If not in cache, read the file
	if !ok {
		var err error
		bytes, err := ioutil.ReadFile(filename)
		if err != nil {
			return "", err
		}

		// Convert to string and store the script in cache
		content = string(bytes)
		scriptCacheMutex.Lock()
		scriptCache[filename] = content
		scriptCacheMutex.Unlock()
	}

	// New lua state
	L := lua.NewState()
	defer L.Close()

	// Register custom Go function that can be called from Lua
	L.SetGlobal("customGoFunction", L.NewFunction(func(L *lua.LState) int {
		// Get argument from Lua
		arg := L.CheckString(1)

		// Do something with the argument
		fmt.Println("customGoFunction called from Lua with arg: " + arg)

		// Return a value to Lua
		L.Push(lua.LString("Go says hello back!"))
		return 1 // Number of return values
	}))

	// Convert map to Lua table and set as global variable
	L.SetGlobal("payload", mapToTable(L, jsonData))

	// Do the lua code
	if err := L.DoString(content); err != nil {
		return "", err
	}

	// Get the lua value
	luaValue := L.Get(-1)
	return luaValue.String(), nil
}

func main() {
	router := gin.Default()

	// Middlewares
	router.Use(static.Serve("/", static.LocalFile("./public", true)))
	router.Use(gin.BasicAuth(gin.Accounts{
		"user1": "password1",
		"user2": "password2",
	}))

	// Rate limiter middleware
	router.Use(func(c *gin.Context) {
		limiter := getVisitor(c.ClientIP())
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}
		c.Next()
	})

	router.POST("/runLuaFile/:filename", func(c *gin.Context) {
		// Get filename from the URL
		filename := c.Param("filename")

		// Check if filename is safe
		if filepath.Base(filename) != filename {
			c.String(http.StatusBadRequest, "Invalid filename")
			return
		}

		// Parse JSON from request body
		var jsonData map[string]interface{}
		err := c.ShouldBindJSON(&jsonData)
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid JSON")
			return
		}

		// Run the Lua script
		result, err := runLuaScript(filename, jsonData)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}

		c.String(http.StatusOK, result)
	})

	router.Run() // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}