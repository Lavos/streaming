package main

import (
	"fmt"
	"os"
	"os/signal"
)


func sub(done chan bool) {

}

func main() {
	fmt.Fprintf(os.Stdout, "Waiting for signal...")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)

	<-sig

	fmt.Fprintf(os.Stdout, "GOT SIGNAL EXITING\n")
}
