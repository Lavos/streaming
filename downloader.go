package streaming

import (
	"log"
	"net/http"
	"io"
)

type Downloader struct {
	Work   chan string
	Output io.Writer
}

func NewDownloader(work chan string, output io.Writer) *Downloader {
	d := &Downloader{
		Work:   work,
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

			io.Copy(d.Output, resp.Body)
			resp.Body.Close()
		}
	}()
}
