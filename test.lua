print("Hello, World!")

local result = customGoFunction("Hello from Lua!")
print(result)

local name = payload["name"]
if name == nil then
    name = "Guest"
end

return "Hello, " .. name .. "!"
