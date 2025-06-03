.PHONY: dev clean

GH_PAGES_DIR := public

dev: build
	@echo "Running dev server"
	go run server/server.go

build:
	@echo "Building application"
	GOOS=js GOARCH=wasm go build -o main.wasm

build-pages: build
	@echo "Building GitHub Pages"
	@mkdir -p $(GH_PAGES_DIR)
	@cp index.html $(GH_PAGES_DIR)/
	@cp wasm_exec.js $(GH_PAGES_DIR)/
	@cp main.wasm $(GH_PAGES_DIR)/
	@cp -r roms $(GH_PAGES_DIR)/roms

clean:
	@echo "Cleaning application"
	@rm main.wasm
	@rm -rf public