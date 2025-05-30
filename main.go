package main

import (
	"fmt"
	"syscall/js"
	"time"
)

const (
	memorySize = 4096
	// (512) start of most CHIP-8 programs
	programOffset = 0x200
	displayWidth  = 64
	displayHeight = 32
	// the display is 64 x 32, so multiply each pixel by the drawScale
	drawScale = 10
	// 60hz - 1 million microseconds in a second
	thresholdMicroseconds = (1.0 / 60.0) * 1000000.0
)

// RAM
var memory [memorySize]uint8

// program counter
var PC uint16

// index register
var I uint16

// stack
var stack []uint16

// general purpose registers
var register [16]uint8

// true for "on", false for "off"
var outputBuffer [displayHeight][displayWidth]bool

// execution loop variables
var timeAccumulator float64
var lastFrameTime time.Time

// for debugging
var romLength int

func main() {
	fmt.Println("Hello Wasm")

	setUpRomLoader()

	fmt.Println("setup complete, blocking main goroutine")
	<-make(chan struct{})
}

//go:wasmexport processEmulatorStep
func processEmulatorStep() {
	now := time.Now()
	delta := now.Sub(lastFrameTime)
	timeAccumulator += float64(delta.Microseconds())

	for timeAccumulator >= thresholdMicroseconds {
		executeInstruction()
		timeAccumulator -= thresholdMicroseconds
	}
}

func executeInstruction() {
	// instructions are 2 bytes
	instruction := (uint16(memory[PC]) << 8) | uint16(memory[PC+1])
	fmt.Printf("PC=[%d], handling instruction [%04X] [%16b]\n", PC, instruction, instruction)

	// look at the first nibble
	switch (instruction & 0xF000) >> 12 {
	case 0x0:
		{
			if uint8(instruction&0x00FF) == 0xE0 {
				fmt.Println("clear screen")
				clearScreen()
			}
		}
	case 0x1:
		{
			// 1NNN : Jump
			// set PC to NNN
			nnn := instruction & 0xFFF
			PC = nnn
			// we don't want to increment PC again, so return here
			return
		}
	case 0x6:
		{
			// set the register VX to the value NN
			registerIndex := (instruction & 0x0F00) >> 8
			value := uint8(instruction & 0x00FF)
			fmt.Printf("setting register %d to [%02X]\n", registerIndex, value)
			register[registerIndex] = value
		}
	case 0x7:
		{
			// 7XNN
			// add the value of NN to VX
			vx := (instruction & 0x0F00) >> 8
			nn := uint8(instruction & 0x00FF)
			fmt.Printf("7XNN -> add nn=[%d] to vx=[%02X]\n", nn, vx)
			register[vx] += nn
		}
	case 0xA:
		{
			fmt.Println("set I to nnn")
			// set I to NNN
			nnn := instruction & 0xFFF
			fmt.Printf("set I to [%012b]\n", nnn)
			I = instruction & 0xFFF
		}
	case 0xD:
		{
			// DXYN draw N pixels tall sprite from memory location I, at X Y coord from register
			// the starting position of the drawing should wrap, but not the actual drawing itself
			vx := (instruction & 0x0F00) >> 8
			vy := (instruction & 0x00F0) >> 4
			n := (instruction & 0x000F) >> 0
			fmt.Printf("drawing DXYN -> [D%X%X%X]\n", vx, vy, n)
			drawSprite(vx, vy, n)
		}
	default:
		{
			fmt.Printf("unhandled instruction: [%04X]\n", instruction)
		}
	}
	PC += 2
}

func drawSprite(xReg uint16, yReg uint16, height uint16) {
	startingX := register[xReg] & 63
	x := startingX
	y := register[yReg] & 63
	fmt.Printf("attempting to draw sprite from %d\n", memory[I])

	pixelWasTurnedOff := false
	for line := I; line < I+height; line++ {
		for i := 7; i >= 0; i-- {
			bitValue := (memory[line] >> i) & 1
			if bitValue != 0 {
				if outputBuffer[y][x] {
					pixelWasTurnedOff = true
				}
				// flip the pixel
				outputBuffer[y][x] = !outputBuffer[y][x]
			}
			x++
		}
		x = startingX
		y++
	}
	if pixelWasTurnedOff {
		register[0xF] = 1
	} else {
		register[0xF] = 0
	}

	goImageData := make([]byte, displayWidth*displayHeight*4)
	i := 0
	for screenY := 0; screenY < displayHeight; screenY++ {
		for screenX := 0; screenX < displayWidth; screenX++ {
			if outputBuffer[screenY][screenX] {
				// write "on" pixel
				goImageData[i] = 238
				goImageData[i+1] = 238
				goImageData[i+2] = 238
				goImageData[i+3] = 255
			} else {
				// write "off" pixel
				goImageData[i] = 32
				goImageData[i+1] = 32
				goImageData[i+2] = 32
				goImageData[i+3] = 255
			}
			i += 4
		}
	}

	// create flat array to pass to ImageData()
	jsUint8Array := js.Global().Get("Uint8Array").New(len(goImageData))
	js.CopyBytesToJS(jsUint8Array, goImageData)
	jsUint8ClampedArray := js.Global().Get("Uint8ClampedArray").New(jsUint8Array)
	jsImageData := js.Global().Get("ImageData").New(jsUint8ClampedArray, displayWidth, displayHeight)

	// create an OffscreenCanvas to pass to drawImage()
	jsOffscreenCanvas := js.Global().Get("OffscreenCanvas").New(displayWidth, displayHeight)
	jsOffscreenCanvasCtx := jsOffscreenCanvas.Call("getContext", "2d")
	jsOffscreenCanvasCtx.Call("putImageData", jsImageData, 0, 0)

	// copy the content of the OffscreenCanvas onto the actual Canvas
	document := js.Global().Get("document")
	canvas := document.Call("getElementById", "canvas")
	ctx := canvas.Call("getContext", "2d")
	ctx.Call("drawImage", jsOffscreenCanvas, 0, 0, displayWidth*drawScale, displayHeight*drawScale)
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
			romLength = copy(memory[programOffset:], goDstSlice)

			fmt.Printf("Loaded %d bytes of ROM into memory array\n", romLength)

			// reset variables
			PC = programOffset
			timeAccumulator = 0.0
			lastFrameTime = time.Now()

			js.Global().Get("onRomLoaded").Invoke()

			return nil
		}))

		return nil
	}))
}

func clearScreen() {
	document := js.Global().Get("document")
	canvas := document.Call("getElementById", "canvas")
	ctx := canvas.Call("getContext", "2d")
	width := canvas.Get("width").Int()
	height := canvas.Get("height").Int()
	ctx.Set("fillStyle", "#222")
	ctx.Call("fillRect", 0, 0, width, height)
}

//go:wasmexport debugRom
func debugRom() {
	fmt.Printf("debugging ROM of length [%d]\n", romLength)
	document := js.Global().Get("document")
	debugModal := document.Call("getElementById", "debug-modal")
	debugModalStyle := debugModal.Get("style")
	debugModalStyle.Set("visibility", "visible")

	table := document.Call("createElement", "table")
	var row js.Value
	var data js.Value

	// header
	row = document.Call("createElement", "tr")
	data = document.Call("createElement", "th")
	data.Call("append", "RAM")
	row.Call("append", data)
	data = document.Call("createElement", "th")
	data.Call("append", "Hex Instr")
	row.Call("append", data)
	data = document.Call("createElement", "th")
	data.Call("append", "Binary Instr")
	row.Call("append", data)
	table.Call("append", row)

	i := programOffset
	for i < programOffset+romLength {
		instruction := (uint16(memory[i]) << 8) | uint16(memory[i+1])
		byte1 := instruction >> 8
		byte2 := instruction & 0xFF

		// RAM
		row = document.Call("createElement", "tr")
		data = document.Call("createElement", "td")
		data.Call("append", i)
		row.Call("append", data)
		// hex
		data = document.Call("createElement", "td")
		data.Call("append", fmt.Sprintf("%02X %02X", byte1, byte2))
		row.Call("append", data)
		// binary
		data = document.Call("createElement", "td")
		data.Call("append", fmt.Sprintf("%08b %08b", byte1, byte2))
		row.Call("append", data)

		table.Call("append", row)
		i += 2
	}

	debugModal.Call("append", table)
}
