package main

import (
	"flag"
	"github.com/Lavos/streaming"
	"log"
	"math"
	"fmt"
	"os"
)

var (
	channelname = flag.String("channel", "gamesdonequick", "The name of the channel.")
	variant     = flag.String("variant", "chunked", "The variant of the stream.")
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

func humanReadable (value int64) string {
/* def sizeof_fmt(num, suffix='B'):
    for unit in ['','Ki','Mi','Gi','Ti','Pi','Ei','Zi']:
        if abs(num) < 1024.0:
            return "%3.1f%s%s" % (num, unit, suffix)
        num /= 1024.0
    return "%.1f%s%s" % (num, 'Yi', suffix)
*/
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

			fmt.Fprintf(os.Stderr, "\033[KBytesTotal: %s, DownloadRate: %s\r", humanReadable(s.BytesTotal), humanReadable(s.BytesPerSecond))

		case <-q:
			done <- true
		}
	}
}
