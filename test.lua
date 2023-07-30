function fib(n)
    if n < 2 then
        return n
    else
        return fib(n - 1) + fib(n - 2)
    end
end

print("Hello, World!")

local result = customGoFunction("Hello from Lua!")
print(result)

local name = payload["name"]
if name == nil then
    name = "Guest"
end

print("Fibonacci sequence up to 25:")
for i = 1, 25 do
    print(fib(i))
end

-- Set a random header
setHeader("X-Custom-Header", "TestValue")

return "Hello, " .. name .. "!"
