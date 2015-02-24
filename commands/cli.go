package main

import (
	"github.com/Lavos/streaming"
	"log"
	"flag"
	"os"
)

var (
	channelname = flag.String("channel", "omnigamer", "The name of the channel.")
	variant = flag.String("variant", "medium", "The variant of the stream.")
)

func awaitQuitKey(done chan bool) {
	var buf [1]byte

	for {
		_, err := os.Stdin.Read(buf[:])

		if err != nil || buf[0] == 'q' {
			done <- true
		}
	}
}

func main() {
	flag.Parse()

	done := make(chan bool)
	status := make(chan string)

	w, err := streaming.New(*channelname, *variant, status)

	log.Printf("Worker: %#v %s", w, err)

	go awaitQuitKey(done)

loop:
	for {
		select {
		case s := <-status:
			log.Print(s)

		case <-done:
			break loop
		}
	}
}
