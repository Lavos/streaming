package main

import (
	"log"
	"github.com/Lavos/streaming"
)

func main () {
	w, err := streaming.New("controllerhead")

	log.Printf("Worker: %#v %s", w, err)

	for id := range w.Status {
		log.Printf("Segment %s downloading.", id)
	}
}
