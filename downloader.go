package streaming

import (
	"log"
	"net/http"
	"io"
)

type Downloader struct {
	Work   chan string
	TransferedBytes chan int64
	Output io.Writer
}

func NewDownloader(work chan string, output io.Writer) *Downloader {
	d := &Downloader{
		Work:   work,
		TransferedBytes: make(chan int64),
		Output: output,
	}

	d.run()
	return d
}

func (d *Downloader) run() {
	go func() {
		var req *http.Request
		var resp *http.Response
		var err error
		var uri string
		var n int64

		for uri = range d.Work {
			req, _ = http.NewRequest("GET", uri, nil)
			resp, err = http.DefaultClient.Do(req)

			if err != nil {
				log.Printf("Got error: %s", err)
				continue
			}

			if resp == nil {
				log.Print("Got nil response.")
				continue
			}

			if resp.StatusCode != 200 {
				log.Printf("Got non-200: %s", resp.Status)
				continue
			}

			n, _ = io.Copy(d.Output, resp.Body)
			d.TransferedBytes <- n
			resp.Body.Close()
		}
	}()
}
