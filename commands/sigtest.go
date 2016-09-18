package main

import (
	"fmt"
	"os"
	"os/signal"
	"os/exec"
)


func sub(done chan bool) {
	cmd := exec.Command("./sub")
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	err := cmd.Start()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not exec sub.\n")
		return
	}

	cmd.Wait()

	fmt.Fprintf(os.Stdout, "Sub wait over.\n")
	done <- true
}

func main() {
	done := make(chan bool)
	go sub(done)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)

	fmt.Fprintf(os.Stdout, "Entering LOOP\n")
loop:
	for {
		select {
		case <-sig:
			fmt.Fprintf(os.Stdout, "Main got signal.\n")

			// break loop

		case <-done:
			fmt.Fprintf(os.Stdout, "Main got done bool.\n")

			break loop
		}
	}
}
