package main

import (
	"github.com/Lavos/streaming"
	"log"
	"flag"
	"os"
)

var (
	channelname = flag.String("channel", "gamesdonequick", "The name of the channel.")
	variant = flag.String("variant", "chunked", "The variant of the stream.")
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

	q := make(chan bool)

	status, done := streaming.Watch(*channelname, *variant, os.Stdout)
	log.Print("Press q<enter> to exit.")

	go awaitQuitKey(q)

loop:
	for {
		select {
		case s, ok := <-status:
			if !ok {
				break loop
			}

			log.Print(s)

		case <-q:
			done <- true
		}
	}
}
