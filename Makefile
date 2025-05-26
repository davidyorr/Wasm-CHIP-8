.PHONY: dev

dev: build
	@echo "Running dev server"
	go run server/server.go

build:
	@echo "Building application"
	GOOS=js GOARCH=wasm go build -o main.wasm

clean:
	@echo "Cleaning application"
	rm main.wasm