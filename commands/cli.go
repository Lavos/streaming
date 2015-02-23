package main

import (
	"log"
	"github.com/Lavos/streaming"
	"time"
)

func main () {
	w, err := streaming.New("darkwing_duck_sda")

	log.Printf("Worker: %#v %s", w, err)
	time.Sleep(10 * time.Minute)
}
