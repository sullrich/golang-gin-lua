#!/bin/bash

ab -k -p post.json -T application/json -c 500 -n 10000 http://localhost:8080/runLuaFile/test.lua

