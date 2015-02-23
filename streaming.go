package streaming

import (
	"encoding/json"
	"fmt"
	"github.com/golang/groupcache/lru"
	"github.com/kz26/m3u8"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"
)

const (
	USHER_API_MASK = "http://usher.twitch.tv/api/channel/hls/%s.m3u8?player=twitchweb&token=%s&sig=%s&$allow_audio_only=true&allow_source=true&type=any&p=%d"
	TOKEN_API_MASK = "http://api.twitch.tv/api/channels/%s/access_token"
)

type Watcher struct {
	ChannelName string
	Status      chan string

	playlistWatcher *PlaylistWatcher
	downloader      *Downloader
}

func New(channelName string) (*Watcher, error) {
	w := &Watcher{
		ChannelName: channelName,
	}

	p, err := NewPlaylistWatcher(w.ChannelName)

	if err != nil {
		return nil, err
	}

	w.playlistWatcher = p
	w.Status = p.Status
	w.downloader = NewDownloader(w.playlistWatcher.Output, os.Stdout)

	return w, nil
}

type PlaylistWatcher struct {
	ChannelName string
	Token       string
	Signature   string
	Output      chan string
	Status      chan string
}

func NewPlaylistWatcher(channelName string) (*PlaylistWatcher, error) {
	p := &PlaylistWatcher{
		ChannelName: channelName,
		Output:      make(chan string, 1024),
		Status:      make(chan string),
	}

	err := p.getToken()

	if err != nil {
		return nil, err
	}

	p.run()

	return p, nil
}

func (p *PlaylistWatcher) getToken() error {
	req, _ := http.NewRequest("GET", fmt.Sprintf(TOKEN_API_MASK, p.ChannelName), nil)

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		fmt.Errorf("Non-200 code returned for TOKEN: %s", resp.StatusCode)
	}

	var t TokenResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&t)

	if err != nil {
		return err
	}

	resp.Body.Close()

	p.Token = t.Token
	p.Signature = t.Signature
	return nil
}

func (p *PlaylistWatcher) run() {
	go func() {
		cache := lru.New(1024)
		base_url, _ := url.Parse(fmt.Sprintf(USHER_API_MASK, p.ChannelName, p.Token, p.Signature, 123456))

		req, _ := http.NewRequest("GET", base_url.String(), nil)
		resp, _ := http.DefaultClient.Do(req)

		/*if err != nil || (resp != nil && resp.StatusCode != 200) {
			log.Printf("Got a response from USHER: %s", resp.Status)
			time.Sleep(5 * time.Second)
		}*/

		playlist, _, _ := m3u8.DecodeFrom(resp.Body, true)
		master_playlist := playlist.(*m3u8.MasterPlaylist)
		resp.Body.Close()

		variant_base, _ := url.Parse(master_playlist.Variants[0].URI)

		for {
			req, _ = http.NewRequest("GET", master_playlist.Variants[0].URI, nil)
			resp, err := http.DefaultClient.Do(req)

			if err != nil || (resp != nil && resp.StatusCode != 200) {
				log.Printf("Got a response from VARIANT: %s", resp.Status)
				time.Sleep(5 * time.Second)
			}

			dir := path.Dir(variant_base.Path)

			playlist, _, _ = m3u8.DecodeFrom(resp.Body, true)
			media_playlist := playlist.(*m3u8.MediaPlaylist)

			resp.Body.Close()

			for _, segment := range media_playlist.Segments {
				if segment != nil {
					_, hit := cache.Get(segment.URI)

					if !hit {
						p.Output <- fmt.Sprintf("%s://%s%s/%s", variant_base.Scheme, variant_base.Host, dir, segment.URI)
						cache.Add(segment.URI, nil)
						p.Status <- segment.URI
					}
				}
			}

			time.Sleep(time.Duration(media_playlist.TargetDuration) * time.Second)
		}
	}()
}

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

		for uri := range d.Work {
			req, _ = http.NewRequest("GET", uri, nil)
			resp, err = http.DefaultClient.Do(req)

			if err == nil {
				io.Copy(d.Output, resp.Body)
				resp.Body.Close()
			}
		}
	}()
}
