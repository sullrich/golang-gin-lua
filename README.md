# golang-gin-lua

## This is a simple golang webserver using gin that invokes lua scripts.

### Testing
```
curl -u user1:password1 -X POST http://localhost:8080/runLuaFile/test.lua -d '{"name":"John Doe"}'
```