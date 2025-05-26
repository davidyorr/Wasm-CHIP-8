package main

import (
	"fmt"
	"syscall/js"
)

func main() {
	fmt.Println("Hello Wasm")

	fmt.Println("go func()")
	go func() {
		fmt.Println("calling draw()")
		draw()
	}()

	fmt.Println("setup complete, blocking main goroutine")
	<-make(chan struct{})
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