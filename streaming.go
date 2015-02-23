package streaming

import (
	"log"
	"fmt"
	"syscall"
	"path"
	"net/url"
	"net/http"
	"encoding/json"
	"github.com/golang/groupcache/lru"
	"github.com/kz26/m3u8"
	"os"
	"io"
	"time"
)

const (
	USHER_API_MASK = "http://usher.twitch.tv/api/channel/hls/%s.m3u8?player=twitchweb&token=%s&sig=%s&$allow_audio_only=true&allow_source=true&type=any&p=%d"
	TOKEN_API_MASK = "http://api.twitch.tv/api/channels/%s/access_token"
)

type Watcher struct {
	ChannelName string

	fifo *os.File
	playlistWatcher *PlaylistWatcher
	downloader *Downloader
}

func New(channelName string) (*Watcher, error) {
	w := &Watcher{
		ChannelName: channelName,
	}

	// start playlist watcher
	p, err := NewPlaylistWatcher(w.ChannelName)

	if err != nil {
		return nil, err
	}

	w.playlistWatcher = p

	err = syscall.Mkfifo("/data/homes/lavos/temp/watcher", 0777)

	log.Print("Mkfifo error: %s", err)

	// file, err := os.Open("/data/homes/lavos/temp/watcher")

	w.downloader = NewDownloader(w.playlistWatcher.Output, os.Stdout)

	return w, nil
}

type PlaylistWatcher struct {
	ChannelName string
	Token string
	Signature string
	Output chan string
}

func NewPlaylistWatcher(channelName string) (*PlaylistWatcher, error) {
	p := &PlaylistWatcher{
		ChannelName: channelName,
		Output: make(chan string, 1024),
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

	log.Printf("Request: %s", req.URL.String())

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		fmt.Errorf("Non-200 code returned: %s", resp.StatusCode)
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

func (p *PlaylistWatcher) run(){
	go func(){
		cache := lru.New(1024)

		for {
			base_url, _ := url.Parse(fmt.Sprintf(USHER_API_MASK, p.ChannelName, p.Token, p.Signature, 123456))

			req, _ := http.NewRequest("GET", base_url.String(), nil)
			resp, err := http.DefaultClient.Do(req)

			log.Printf("error: %s", err)

			playlist, _, _ := m3u8.DecodeFrom(resp.Body, true)
			master_playlist := playlist.(*m3u8.MasterPlaylist)
			resp.Body.Close()

			variant_base, _ := url.Parse(master_playlist.Variants[0].URI)

			req, _ = http.NewRequest("GET", master_playlist.Variants[0].URI, nil)
			resp, err = http.DefaultClient.Do(req)

			log.Printf("error: %s", err)

			dir := path.Dir(variant_base.Path)

			playlist, _, _ = m3u8.DecodeFrom(resp.Body, true)
			media_playlist := playlist.(*m3u8.MediaPlaylist)

			log.Printf("TargetDuration: %f", media_playlist.TargetDuration)
			resp.Body.Close()

			for _, segment := range media_playlist.Segments {
				if segment != nil {
					_, hit := cache.Get(segment.URI)

					if !hit {
						p.Output <- fmt.Sprintf("%s://%s%s/%s", variant_base.Scheme, variant_base.Host, dir, segment.URI)
						cache.Add(segment.URI, nil)
					}
				}
			}

			time.Sleep(time.Duration(media_playlist.TargetDuration) * time.Second)
		}
	}()
}

type Downloader struct {
	Work chan string
	Output io.Writer
}

func NewDownloader (work chan string, output io.Writer) *Downloader {
	d := &Downloader{
		Work: work,
		Output: output,
	}

	d.run()
	return d
}

func (d *Downloader) run () {
	go func(){
		log.Printf("Running Downloader.")

		for uri := range d.Work {
			log.Printf("WORK URI: %s", uri)
			req, _ := http.NewRequest("GET", uri, nil)
			resp, err := http.DefaultClient.Do(req)

			log.Printf("Response: %#v, Error: %s", resp, err)

			if err == nil {
				io.Copy(d.Output, resp.Body)
				resp.Body.Close()
			}
		}
	}()
}
