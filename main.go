package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: wrikery <command>")
		return
	}
	fmt.Println("hello from wrikery:", os.Args[1])
}