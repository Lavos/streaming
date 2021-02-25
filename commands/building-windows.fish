#!/usr/bin/fish

env GOOS=windows GOARCH=amd64 go build -o streaming.exe cli.go
