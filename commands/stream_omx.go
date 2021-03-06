package main

import (
	"flag"
	"github.com/Lavos/streaming"
	"fmt"
	"log"
	"syscall"
	"os"
	"os/exec"
	"time"

	"github.com/kelseyhightower/envconfig"
)


var (
	hdmi = flag.Bool("hdmi", false, "Output audio through HDMI.")
	channelname = flag.String("channel", "gamesdonequick", "The name of the channel.")
	variant     = flag.String("variant", "chunked", "The variant of the stream.")
	sector = flag.Int("sector", -1, "The location on the screen to play the video.")

	sectors = []string{
		"0,0,1067,600",
		"853,480,1920,1080",
		"1067,0,1920,480",
		"0,600,853,1080",
	}

	fifo_path string
)

func omx(done chan bool) {
	var err error

	// exec omxplayer
	args := []string{
		"-o",
	}

	if *hdmi {
		args = append(args, "hdmi")
	} else {
		args = append(args, "local")
	}

	if *sector != -1 {
		if *sector <= len(sectors) - 1 {
			dims := sectors[*sector]
			args = append(args, []string{"--win", dims, "--layer", fmt.Sprintf("%d", *sector)}...)
		}
	}

	args = append(args, fifo_path)

	cmd := exec.Command("omxplayer", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	err = cmd.Start()

	if err != nil {
		log.Fatal(err)
	}

	cmd.Wait()
	done <- true
}

func main() {
	var c streaming.Configuration
	envconfig.MustProcess("TWITCH", &c)

	flag.Parse()

	omx_done := make(chan bool)
	go omx(omx_done)

	// make fifo
	fifo_path = fmt.Sprintf("/tmp/stream_%d.fifo", time.Now().UTC().UnixNano())
	syscall.Mkfifo(fifo_path, 0722)

	// open fifo
	file, err := os.OpenFile(fifo_path, os.O_WRONLY, 0)

	if err != nil {
		log.Fatalf("Could not open fifo `%s`. Error: %s", fifo_path, err)
	}

	log.Printf("fifo `%s` opened.", fifo_path)

	// send stream data to fifo
	p := streaming.NewPlaylister(c, *channelname, *variant, file)

	status := p.Status()

loop:
	for {
		select {
		case _, ok := <-status:
			if !ok {
				break loop
			}

		case <-omx_done:
			p.Done()
		}
	}
}
