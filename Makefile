.PHONY: dev clean

dev: build
	@echo "Running dev server"
	go run server/server.go

build:
	@echo "Building application"
	GOOS=js GOARCH=wasm go build -o main.wasm

build-pages: build
	@echo "Building GitHub Pages"
	mkdir public
	cp {index.html,wasm_exec.js,main.wasm} public

clean:
	@echo "Cleaning application"
	rm main.wasm
	rm -rf public