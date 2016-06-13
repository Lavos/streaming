package main

import (
	"flag"
	"github.com/Lavos/streaming"
	"math"
	"fmt"
	"log"
	"syscall"
	"os"
	"os/signal"
	"os/exec"
	"time"
)


var (
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

func humanReadable (value int64) string {
	num := float64(value)
	units := []string{"", "Ki", "Mi", "Gi", "Ti", "Pi", "Ei", "Zi"}

	for _, unit := range units {
		if math.Abs(num) < 1024.0 {
			return fmt.Sprintf("%3.1f%sB", num, unit)
		}

		num = num / 1024.0
	}

	return fmt.Sprintf("%.1fYiB", num)
}

func omx() {
	var err error

	// exec omxplayer
	args := []string{
		"-o", "local",
	}

	if *sector != -1 {
		if *sector < len(sectors) - 1 {
			dims := sectors[*sector]
			args = append(args, []string{"--win", dims, "--layer", fmt.Sprintf("%d", *sector)}...)
		}
	}

	args = append(args, fifo_path)

	cmd := exec.Command("omxplayer", args...)
	cmd.Stderr = os.Stderr

	err = cmd.Start()

	fmt.Printf("Command: %#v\n", cmd)

	if err != nil {
		log.Fatal(err)
	}

	cmd.Wait()
}

func main() {
	flag.Parse()

	go omx()

	// make fifo
	fifo_path = fmt.Sprintf("/tmp/stream_%d.fifo", time.Now().UTC().UnixNano())
	syscall.Mkfifo(fifo_path, 0722)

	// open fifo
	fmt.Printf("here.\n")

	file, err := os.Open(fifo_path)

	fmt.Printf("here 2.\n")

	if err != nil {
		log.Fatalf("Could not open fifo `%s`. Error: %s", fifo_path, err)
	}

	log.Printf("fifo `%s` opened.", fifo_path)

	// send stream data to fifo
	status, done := streaming.Watch(*channelname, *variant, file)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)

loop:
	for {
		select {
		case s, ok := <-status:
			if !ok {
				break loop
			}

			fmt.Fprintf(os.Stderr, "\033[KBytesTotal: %s, DownloadRate: %s\r", humanReadable(s.BytesTotal), humanReadable(s.BytesPerSecond))

		case <-sig:
			done <- true
		}
	}
}
