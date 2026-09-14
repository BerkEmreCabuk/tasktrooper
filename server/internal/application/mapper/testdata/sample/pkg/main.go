package main

import "fmt"

func main() {
	fmt.Println("hello")
}

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h *Hello) Greet() string {
	return "hello"
}

const Version = "1.0.0"
