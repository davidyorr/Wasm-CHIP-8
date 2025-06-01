package main

import (
	"fmt"
	"math/rand/v2"
	"syscall/js"
	"time"
)

const (
	memorySize = 4096
	// (512) start of most CHIP-8 programs
	programOffset = 0x200
	fontOffset    = 0x050
	displayWidth  = 64
	displayHeight = 32
	// the display is 64 x 32, so multiply each pixel by the drawScale
	drawScale = 10
	// 60Hz - 1 million microseconds in a second
	thresholdMicroseconds = (1.0 / 60.0) * 1000000.0
)

// RAM
var memory [memorySize]uint8

// program counter
var PC uint16

// general purpose registers, referred to as VX, where X is a hex digit
var V [16]uint8

// timers
var delayTimer uint8
var soundTimer uint8

// index register, used to store memory addresses
var I uint16

// stack
var stack []uint16

// true for "on", false for "off"
var outputBuffer [displayHeight][displayWidth]bool

// execution loop variables
var timeAccumulator float64
var lastFrameTime time.Time

// to keep track of which keys are currently pressed, true for pressed
var keypadStates [16]bool

// for debugging
var debugMode bool = false
var romLength int
var romIsRunning bool = false

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
		// target 660 IPS
		// 60Hz is timers target, so execute 11 instructions per tick
		for range 11 {
			executeInstruction()
			if debugMode {
				drawDebugInformation()
			}
		}
		// decrement timers once per 60Hz
		if delayTimer > 0 {
			delayTimer--
		}
		if soundTimer > 0 {
			soundTimer--
		}
		timeAccumulator -= thresholdMicroseconds
	}
	lastFrameTime = time.Now()
}

func executeInstruction() {
	if !romIsRunning {
		return
	}

	// instructions are 2 bytes
	instruction := (uint16(memory[PC]) << 8) | uint16(memory[PC+1])
	// fmt.Printf("PC=[%d], handling instruction [%04X] [%16b]\n", PC, instruction, instruction)

	// look at the first nibble
	switch (instruction & 0xF000) >> 12 {
	case 0x0:
		{
			// 0NNN is only used on certain old computers

			switch uint8(instruction & 0x00FF) {
			case 0xE0:
				{
					// 00E0 : clear screen
					clearScreen()

				}
			case 0xEE:
				{
					// 00EE : return from subroutine
					PC, stack = stack[len(stack)-1], stack[:len(stack)-1]
				}
			default:
				{
					stopForUnhandledInstruction(instruction)
					return
				}
			}
		}
	case 0x1:
		{
			// 1NNN : jump
			// set PC to NNN
			nnn := instruction & 0xFFF
			PC = nnn
			// we don't want to increment PC again, so return here
			return
		}
	case 0x2:
		{
			// 2NNN : subroutine
			// call subroutine at NNN
			nnn := instruction & 0xFFF
			stack = append(stack, PC)
			PC = nnn
			return
		}
	case 0x3:
		{
			// 3XNN : skip conditional
			// skip the next instruction if VX equals NN
			vx := V[(instruction&0x0F00)>>8]
			nn := uint8(instruction & 0x00FF)
			if vx == nn {
				PC += 2
			}
		}
	case 0x4:
		{
			// 4XNN : skip conditional
			// skip the next instruction if VX does equals NN
			vx := V[(instruction&0x0F00)>>8]
			nn := uint8(instruction & 0x00FF)
			if vx != nn {
				PC += 2
			}
		}
	case 0x5:
		{
			// 5XY0 : skip conditional
			// skip the next instruction if VX equals VY
			vx := V[(instruction&0x0F00)>>8]
			vy := V[(instruction&0x00F0)>>4]
			if vx == vy {
				PC += 2
			}
		}
	case 0x6:
		{
			// 6XNN : set
			// set the register VX to the value NN
			x := (instruction & 0x0F00) >> 8
			nn := uint8(instruction & 0x00FF)
			V[x] = nn
		}
	case 0x7:
		{
			// 7XNN : add
			// add the value of NN to VX
			x := (instruction & 0x0F00) >> 8
			nn := uint8(instruction & 0x00FF)
			V[x] += nn
		}
	case 0x8:
		{
			switch (instruction & 0x000F) >> 0 {
			case 0x0:
				{
					// 8XY0 : set
					// set VX to the value of VY
					x := (instruction & 0x0F00) >> 8
					y := (instruction & 0x00F0) >> 4
					V[x] = V[y]
				}
			case 0x1:
				{
					// 8XY1 : bitwise OR
					// set VX to the OR of VX and VY
					x := (instruction & 0x0F00) >> 8
					y := (instruction & 0x00F0) >> 4
					V[x] = V[x] | V[y]
				}
			case 0x2:
				{
					// 8XY2 : bitwise AND
					// set VX to the AND of VX and VY
					x := (instruction & 0x0F00) >> 8
					y := (instruction & 0x00F0) >> 4
					V[x] = V[x] & V[y]
				}
			case 0x3:
				{
					// 8XY3 : bitwise XOR
					// set VX to the XOR of VX and VY
					x := (instruction & 0x0F00) >> 8
					y := (instruction & 0x00F0) >> 4
					V[x] = V[x] ^ V[y]
				}
			case 0x4:
				{
					// 8XY4 : add
					// set VX to the sum of VX and VY
					// if the result overflowed, set VF to 1
					x := (instruction & 0x0F00) >> 8
					y := (instruction & 0x00F0) >> 4
					V[x] = V[x] + V[y]
					if V[x] < V[y] {
						V[0xF] = 1
					} else {
						V[0xF] = 0
					}
				}
			case 0x5:
				{
					// 8XY5 : subtract
					// set VX to the difference of VX and VY
					x := (instruction & 0x0F00) >> 8
					y := (instruction & 0x00F0) >> 4
					if V[x] > V[y] {
						V[0xF] = 1
					} else {
						V[0xF] = 0
					}
					V[x] = V[x] - V[y]
				}
			case 0x6:
				{
					// 8XY6 : shift
					// shift the value of VX one bit to the right
					x := (instruction & 0x0F00) >> 8
					shiftedOutBit := V[x] & 1
					V[0xF] = shiftedOutBit
					V[x] = V[x] >> 1
				}
			case 0x7:
				{
					// 8XY7 : subtract
					// set VX to the difference of VY and VX
					x := (instruction & 0x0F00) >> 8
					y := (instruction & 0x00F0) >> 4
					if V[y] > V[x] {
						V[0xF] = 1
					} else {
						V[0xF] = 0
					}
					V[x] = V[y] - V[x]
				}
			case 0xE:
				{
					// 8XYE : shift
					// shift the value of VX one bit to the left
					x := (instruction & 0x0F00) >> 8
					shiftedOutBit := V[x] >> 7
					V[0xF] = shiftedOutBit
					V[x] = V[x] << 1
				}
			default:
				{
					stopForUnhandledInstruction(instruction)
					return
				}
			}
		}
	case 0x9:
		{
			// 9XY0 : skip conditional
			// skip the next instruction if VX does equals VY
			x := (instruction & 0x0F00) >> 8
			y := (instruction & 0x00F0) >> 4
			if V[x] != V[y] {
				PC += 2
			}
		}
	case 0xA:
		{
			// ANNN : set index
			// set I to NNN
			nnn := instruction & 0xFFF
			I = nnn
		}
	case 0xB:
		{
			// BNNN : jump with offset
			nnn := instruction & 0xFFF
			PC = nnn + uint16(V[0])
			return
		}
	case 0xC:
		{
			// CXNN : random
			// set VX to the result of a random number bitwise ANDed with NN
			x := (instruction & 0x0F00) >> 8
			randomNumber := uint8(rand.UintN(256))
			nn := uint8(instruction & 0x00FF)
			V[x] = randomNumber & nn
		}
	case 0xD:
		{
			// DXYN : draw
			// draw N pixels tall sprite from memory location I, at X Y coord from register
			// the starting position of the drawing should wrap, but not the actual drawing itself
			x := (instruction & 0x0F00) >> 8
			y := (instruction & 0x00F0) >> 4
			n := (instruction & 0x000F) >> 0
			drawSprite(x, y, n)
		}
	case 0xE:
		{
			switch uint8(instruction & 0x00FF) {
			case 0x9E:
				{
					// EX9E : skip if key
					// skip one instruction if the VX key is currently pressed
					x := (instruction & 0x0F00) >> 8
					if keypadStates[V[x]] {
						PC += 2
					}
				}
			case 0xA1:
				{
					// EXA1 : skip if key
					// skip one instruction if the VX key is not currently pressed
					x := (instruction & 0x0F00) >> 8
					if !keypadStates[V[x]] {
						PC += 2
					}
				}
			default:
				{
					stopForUnhandledInstruction(instruction)
					return
				}
			}
		}
	case 0xF:
		{
			switch uint8(instruction & 0x00FF) {
			case 0x0A:
				{
					// FX0A : get key
					// stop executing until a key is pressed,
					// then store the value of that key in VX
					x := (instruction & 0x0F00) >> 8
					var keyPtr *uint8 = nil
					for i, pressed := range keypadStates {
						if pressed {
							hexValue := uint8(i)
							keyPtr = &hexValue
							break
						}
					}
					if keyPtr == nil {
						return
					}
					V[x] = *keyPtr
				}
			case 0x07:
				{
					// FX07 : timer
					// set VX to the value of the delay timer
					x := (instruction & 0x0F00) >> 8
					V[x] = delayTimer
				}
			case 0x15:
				{
					// FX15 : timer
					// set the delay timer to the value in VX
					x := (instruction & 0x0F00) >> 8
					delayTimer = V[x]
				}
			case 0x18:
				{
					// FX18 : timer
					// set the sound timer to the value in VX
					x := (instruction & 0x0F00) >> 8
					soundTimer = V[x]
				}
			case 0x1E:
				{
					// FX1E : add to index
					// add the value of VX to index register I
					x := (instruction & 0x0F00) >> 8
					I += uint16(V[x])
				}
			case 0x29:
				{
					// FX29 : font character
					// set I to the address of the hex character in VX (take the last nibble)
					x := (instruction & 0x0F00) >> 8
					I = uint16((V[x] & 0x000F) >> 4)
				}
			case 0x55:
				{
					// FX55 : store memory
					// copy the values of V0 through VX into memory, starting at address I
					x := (instruction & 0x0F00) >> 8
					for j := range x + 1 {
						memory[I+j] = V[j]
					}
				}
			case 0x65:
				{
					// FX65 : load memory
					// read values from memory starting at address I into V0 through VX
					x := (instruction & 0x0F00) >> 8
					for j := range x + 1 {
						V[j] = memory[I+j]
					}
				}
			default:
				{
					stopForUnhandledInstruction(instruction)
					return
				}
			}
		}
	default:
		{
			stopForUnhandledInstruction(instruction)
			return
		}
	}
	PC += 2
}

func drawSprite(xReg uint16, yReg uint16, height uint16) {
	startingX := V[xReg] & 63
	x := startingX
	y := V[yReg] & 63

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
		V[0xF] = 1
	} else {
		V[0xF] = 0
	}

	copyOutputBufferToCanvas()
}

func clearScreen() {
	for y := 0; y < displayHeight; y++ {
		for x := displayWidth - 1; x >= 0; x-- {
			outputBuffer[y][x] = false
		}
	}

	copyOutputBufferToCanvas()
}

func copyOutputBufferToCanvas() {
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

//go:wasmexport setKeypadState
func setKeypadState(key uint32, state bool) {
	if key > 0xF {
		fmt.Println("unsupported key input:", key)
		return
	}
	keypadStates[key] = state
}

func stopForUnhandledInstruction(instruction uint16) {
	fmt.Printf("unhandled instruction: [%04X]\n", instruction)
	js.Global().Get("onUnhandledInstruction").Invoke()
	romIsRunning = false
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
			romIsRunning = true
			loadFont()

			js.Global().Get("onRomLoaded").Invoke()

			return nil
		}))

		return nil
	}))
}

func loadFont() {
	font := [16 * 5]uint8{
		0xF0, 0x90, 0x90, 0x90, 0xF0, // 0
		0x20, 0x60, 0x20, 0x20, 0x70, // 1
		0xF0, 0x10, 0xF0, 0x80, 0xF0, // 2
		0xF0, 0x10, 0xF0, 0x10, 0xF0, // 3
		0x90, 0x90, 0xF0, 0x10, 0x10, // 4
		0xF0, 0x80, 0xF0, 0x10, 0xF0, // 5
		0xF0, 0x80, 0xF0, 0x90, 0xF0, // 6
		0xF0, 0x10, 0x20, 0x40, 0x40, // 7
		0xF0, 0x90, 0xF0, 0x90, 0xF0, // 8
		0xF0, 0x90, 0xF0, 0x10, 0xF0, // 9
		0xF0, 0x90, 0xF0, 0x90, 0x90, // A
		0xE0, 0x90, 0xE0, 0x90, 0xE0, // B
		0xF0, 0x80, 0x80, 0x80, 0xF0, // C
		0xE0, 0x90, 0x90, 0x90, 0xE0, // D
		0xF0, 0x80, 0xF0, 0x80, 0xF0, // E
		0xF0, 0x80, 0xF0, 0x80, 0x80, // F
	}
	iMemory := fontOffset
	iFont := 0
	for iMemory < fontOffset+len(font) {
		memory[iMemory] = font[iFont]
		iMemory++
		iFont++
	}
}

//go:wasmexport debug
func debug() {
	fmt.Println("toggling debug mode...")
	debugMode = !debugMode
}

var keypadIndexToKey = map[int]string{
	0:  "1",
	1:  "2",
	2:  "3",
	3:  "4",
	4:  "q",
	5:  "w",
	6:  "e",
	7:  "r",
	8:  "a",
	9:  "s",
	10: "d",
	11: "f",
	12: "z",
	13: "x",
	14: "c",
	15: "v",
}

func drawDebugInformation() {
	document := js.Global().Get("document")
	canvas := document.Call("getElementById", "debug-canvas")
	ctx := canvas.Call("getContext", "2d")
	width := canvas.Get("width").Int()
	height := canvas.Get("height").Int()
	ctx.Set("fillStyle", "#222")
	ctx.Call("fillRect", 0, 0, width, height)

	leftColumnX := 212.0
	rightColumnX := 252.0
	startingY := 0.0
	lineHeight := 34.0
	line := 1.0
	ctx.Set("font", "28px monospace")

	var writeHeader = func(text any) {
		ctx.Set("textAlign", "right")
		ctx.Call("fillText", text, leftColumnX, startingY+(line*lineHeight))
	}
	var writeLine = func(text any) {
		ctx.Set("textAlign", "left")
		ctx.Call("fillText", text, rightColumnX, startingY+(line*lineHeight))
	}

	// PC
	ctx.Set("fillStyle", "#FF3030")
	writeHeader("PC")
	writeLine(PC)
	line += 1.5

	// I
	ctx.Set("fillStyle", "#559A70")
	writeHeader("I")
	writeLine(I)
	line += 1.5

	// V
	ctx.Set("fillStyle", "#CCAC00")
	writeHeader("V")
	var registerLine string = ""
	for i, value := range V {
		registerLine += fmt.Sprintf("V%X=0x%03X ", i, value)
		if (i+1)%2 == 0 {
			writeLine(registerLine)
			line++
			registerLine = ""
		}
	}
	line += 0.5

	// keypad states
	ctx.Set("fillStyle", "#0099CC")
	writeHeader("keypad")
	var keypadStateLine string = ""
	for i, pressed := range keypadStates {
		state := 0
		if pressed {
			state = 1
		}
		keypadStateLine += fmt.Sprintf("% s=%d ", keypadIndexToKey[i], state)
		if (i+1)%4 == 0 {
			writeLine(keypadStateLine)
			line++
			keypadStateLine = ""
		}
	}
	line += 0.5

	// delay timer
	ctx.Set("fillStyle", "#CC69C8")
	writeHeader("delay timer")
	writeLine(delayTimer)
	line += 1.5

	// sound timer
	ctx.Set("fillStyle", "#7AC4CC")
	writeHeader("sound timer")
	writeLine(soundTimer)
}

//go:wasmexport viewRom
func viewRom() {
	fmt.Printf("viewing ROM of length [%d]\n", romLength)
	document := js.Global().Get("document")
	debugModal := document.Call("getElementById", "rom-viewer-modal")
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
