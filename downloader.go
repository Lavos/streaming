package streaming

import (
	"io"
	"log"
	"net/http"
	"time"
)

type Downloader struct {
	Work   chan string
	Output io.Writer

	statusChan chan Status
}

func NewDownloader(work chan string, status chan Status, output io.Writer) *Downloader {
	d := &Downloader{
		Work:   work,
		Output: output,

		statusChan: status,
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
		var bytesTotal int64
		var start time.Time
		var elapsed float64
		var bytesPerSecond int64

		for uri = range d.Work {
			req, _ = http.NewRequest("GET", uri, nil)
			resp, err = http.DefaultClient.Do(req)

			if err != nil {
				log.Printf("[dl] Got error: %s", err)
				continue
			}

			if resp == nil {
				log.Print("[dl] Got nil response.")
				continue
			}

			if resp.StatusCode != 200 {
				log.Printf("[dl] Got non-200: %s", resp.Status)
				continue
			}

			start = time.Now()
			n, _ = io.Copy(d.Output, resp.Body)
			bytesTotal += n

			elapsed = time.Now().Sub(start).Seconds()

			if elapsed != 0 {
				bytesPerSecond = int64(float64(n) / elapsed)
			}

			d.statusChan <- Status{
				BytesTotal: bytesTotal,
				BytesPerSecond: bytesPerSecond,
				LastFile: uri,
			}

			resp.Body.Close()
		}
	}()
}
