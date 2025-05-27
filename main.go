package main

import (
	"fmt"
	"syscall/js"
)

const (
	memorySize = 4096
	// (512) start of most CHIP-8 programs
	programOffset = 0x200
)

// RAM
var memory [memorySize]uint8
// program counter
var PC uint16
// index register
var I uint16
// stack
var stack []uint16

func main() {
	fmt.Println("Hello Wasm")

	setUpRomLoader()

	go func() {
		draw()
	}()

	fmt.Println("setup complete, blocking main goroutine")
	<-make(chan struct{})
}

func setUpRomLoader() {
	document := js.Global().Get("document")
	fileInput := document.Call("getElementById", "rom-input")
	fileInput.Set("oninput", js.FuncOf(func(this js.Value, args []js.Value) any {
		fileInput.Get("files").Call("item", 0).Call("arrayBuffer").Call("then", js.FuncOf(func(this js.Value, args []js.Value) any {
			jsRomData := js.Global().Get("Uint8Array").New(args[0])
			goDstSlice := make([]byte, jsRomData.Get("length").Int())
			js.CopyBytesToGo(goDstSlice, jsRomData)
			fmt.Println("data", jsRomData)

			// clear any memory from previous ROM
			clear(memory[programOffset:])

			// copy the ROM data into the memory variable
			numberOfBytesCopied := copy(memory[programOffset:], goDstSlice)

			fmt.Printf("Loaded %d bytes of ROM into memory array\n", numberOfBytesCopied)

			return nil
		}))

		return nil
	}))
}

func draw() {
	document := js.Global().Get("document")
	canvas := document.Call("getElementById", "canvas")
	ctx := canvas.Call("getContext", "2d")
	width := canvas.Get("width").Int()
	height := canvas.Get("height").Int()
	ctx.Set("fillStyle", "#EEE")
	ctx.Call("fillRect", 0, 0, width, height)
}