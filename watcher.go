package streaming

import (
	"io"
)

func Watch(channelName string, variant string, out io.Writer) (chan string, chan bool) {
	p := NewPlaylister(channelName, variant)
	NewDownloader(p.Output, out)

	return p.Status, p.Done
}
