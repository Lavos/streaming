package streaming

import (
	"io"
)

func Watch(channelName string, variant string, out io.Writer) (chan Status, chan bool) {
	p := NewPlaylister(channelName, variant)
	NewDownloader(p.Output, p.Status, out)

	return p.Status, p.Done
}
