package main

import (
	"flag"
	"github.com/Lavos/streaming"
	"math"
	"fmt"
	"os"
	"os/signal"

	"github.com/kelseyhightower/envconfig"
)

var (
	channelname = flag.String("channel", "gamesdonequick", "The name of the channel.")
	variant     = flag.String("variant", "chunked", "The variant of the stream.")
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

func main() {
	var c streaming.Configuration
	envconfig.MustProcess("TWITCH", &c)

	flag.Parse()

	// send stream data to STDOUT
	p := streaming.NewPlaylister(c, *channelname, *variant, os.Stdout)

	status := p.Status()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)

loop:
	for {
		select {
		case s, ok := <-status:
			if !ok {
				break loop
			}

			fmt.Fprintf(os.Stderr, "\033[KChannel: %s, KBytesTotal: %s, DownloadRate: %s\r", *channelname, humanReadable(s.BytesTotal), humanReadable(s.BytesPerSecond))

		case <-sig:
			p.Done()
			break loop
		}
	}
}
