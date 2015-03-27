package streaming

import (
	"io"
)

func Watch(channelName string, variant string, out io.Writer) (chan string, chan int64, chan bool) {
	p := NewPlaylister(channelName, variant)
	d := NewDownloader(p.Output, out)

	return p.Status, d.TransferedBytes, p.Done
}
