package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yuin/gopher-lua"
	"golang.org/x/time/rate"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"
)

type ScriptStatus struct {
	Finished bool
	Result   string
	Error    error
}

var scriptStatuses = make(map[string]*ScriptStatus)

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

	// Register httpPost Go function that can be called from Lua
	L.SetGlobal("httpPost", L.NewFunction(func(L *lua.LState) int {
		// Get arguments from Lua
		url := L.CheckString(1)
		body := L.CheckTable(2)

		// Convert lua table to map
		var bodyMap map[string]interface{}
		body.ForEach(func(k lua.LValue, v lua.LValue) {
			bodyMap[k.String()] = v.String()
		})

		// Convert map to json
		bodyJson, err := json.Marshal(bodyMap)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("Failed to convert body to JSON: " + err.Error()))
			return 2
		}

		// Create a new http client
		client := &http.Client{}

		// Create the request
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyJson))
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("Failed to create request: " + err.Error()))
			return 2
		}

		// Send the request
		resp, err := client.Do(req)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("Failed to send request: " + err.Error()))
			return 2
		}
		defer resp.Body.Close()

		// Read the response
		responseData, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("Failed to read response: " + err.Error()))
			return 2
		}

		// Return response to Lua
		L.Push(lua.LString(string(responseData)))
		return 1 // Number of return values
	}))

	// Create and register functions for Lua scripts to use.
	L.SetGlobal("setHeader", L.NewFunction(func(L *lua.LState) int {
		// Create new header
		key := L.CheckString(1)
		value := L.CheckString(2)

		// Store header in Lua's global table
		headers := L.GetGlobal("headers").(*lua.LTable)
		headers.RawSetString(key, lua.LString(value))
		return 0 // Number of return values
	}))

	L.SetGlobal("httpGet", L.NewFunction(func(L *lua.LState) int {
		url := L.CheckString(1)
		client := &http.Client{}

		// Retrieve headers from Lua's global table
		headers := L.GetGlobal("headers").(*lua.LTable)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("Failed to create request: " + err.Error()))
			return 2 // Number of return values
		}

		// Set headers in request
		headers.ForEach(func(key lua.LValue, value lua.LValue) {
			req.Header.Set(key.String(), value.String())
		})

		resp, err := client.Do(req)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("Failed to do request: " + err.Error()))
			return 2 // Number of return values
		}
		defer resp.Body.Close()

		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("Failed to read response body: " + err.Error()))
			return 2 // Number of return values
		}

		L.Push(lua.LString(string(bodyBytes)))
		return 1 // Number of return values
	}))

	// Initialize headers table
	L.SetGlobal("headers", L.NewTable())

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

	router.Use(CORSMiddleware())
	router.Use(loggingMiddleware())

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

	router.GET("/status/:id", func(c *gin.Context) {
		// Get id from the URL
		id := c.Param("id")

		status, ok := scriptStatuses[id]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "script not found"})
			return
		}

		// Note: You need to be careful about concurrently reading/writing to the script status.
		// Consider adding a mutex lock or similar for thread safety.
		if status.Finished {
			c.JSON(http.StatusOK, gin.H{
				"finished": true,
				"result":   status.Result,
				"error":    status.Error,
			})
		} else {
			c.JSON(http.StatusOK, gin.H{"finished": false})
		}
	})

	router.POST("/runLuaFileAsync/:filename", func(c *gin.Context) {
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

		// Generate a unique ID for this script execution
		id := uuid.New().String()

		// Create a new ScriptStatus
		scriptStatuses[id] = &ScriptStatus{
			Finished: false,
		}

		// Start a goroutine to run the script
		go func() {
			result, err := runLuaScript(filename, jsonData)
			// Update the status when the script is done
			scriptStatuses[id].Finished = true
			scriptStatuses[id].Result = result
			if err != nil {
				scriptStatuses[id].Error = err
			}
		}()

		// Return the ID to the client
		c.String(http.StatusOK, id)
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

func loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		startTime := time.Now()

		// Process request
		c.Next()

		// Log request details
		endTime := time.Now()
		log.Printf("[%s] %s %s %s %s",
			endTime.Format(time.RFC3339),
			c.Request.Method,
			c.Request.URL,
			c.ClientIP(),
			endTime.Sub(startTime),
		)
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
