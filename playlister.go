package streaming

import (
	"encoding/json"
	"fmt"
	"github.com/golang/groupcache/lru"
	"github.com/kz26/m3u8"
	// "log"
	"net/http"
	"net/url"
	"path"
	"time"
	"strings"
)

const (
	USHER_API_MASK = "http://usher.twitch.tv/api/channel/hls/%s.m3u8?player=twitchweb&token=%s&sig=%s&$allow_audio_only=true&allow_source=true&type=any&p=%d"
	TOKEN_API_MASK = "http://api.twitch.tv/api/channels/%s/access_token"
)

type Playlister struct {
	ChannelName string
	DesiredVariant string
	Variant *m3u8.Variant

	Token       string
	Signature   string

	Output      chan string
	Status      chan string
	Done	    chan bool
}

func NewPlaylister(channelName, variant string) *Playlister {
	p := &Playlister{
		ChannelName: channelName,
		DesiredVariant: variant,

		Output:      make(chan string, 1024),
		Status: make(chan string),
		Done: make(chan bool),
	}

	p.run()

	return p
}

func (p *Playlister) getToken() error {
	req, _ := http.NewRequest("GET", fmt.Sprintf(TOKEN_API_MASK, p.ChannelName), nil)

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("Non-200 code returned for TOKEN: %s", resp.StatusCode)
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

func (p *Playlister) getVariant() error {
	base_url, _ := url.Parse(fmt.Sprintf(USHER_API_MASK, p.ChannelName, p.Token, p.Signature, 123456))
	req, _ := http.NewRequest("GET", base_url.String(), nil)

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return err
	}

	if resp != nil && resp.StatusCode != 200 {
		return fmt.Errorf("Got a response from USHER: %s", resp.Status)
	}

	playlist, _, _ := m3u8.DecodeFrom(resp.Body, true)
	master_playlist := playlist.(*m3u8.MasterPlaylist)
	resp.Body.Close()

	variants := make([]string, len(master_playlist.Variants))
	var target_variant *m3u8.Variant

	for i, variant := range master_playlist.Variants {
		if variant != nil {
			variants[i] = variant.Video

			if p.DesiredVariant == variant.Video {
				target_variant = variant
			}
		}
	}

	p.Status <- fmt.Sprintf("Variants found: %s", strings.Join(variants, " "))

	if target_variant == nil {
		return fmt.Errorf("Variant specified not found for this stream.")
	}

	p.Variant = target_variant
	return nil
}

func (p *Playlister) run() {
	go func() {
		err := p.getToken()

		if err != nil {
			p.Status <- err.Error()
			close(p.Status)
			return
		}

		err = p.getVariant()

		if err != nil {
			p.Status <- err.Error()
			close(p.Status)
			return
		}

		cache := lru.New(1024)
		variant_url, _ := url.Parse(p.Variant.URI)

		var req *http.Request
		var resp *http.Response
		var dir string
		var playlist m3u8.Playlist
		var media_playlist *m3u8.MediaPlaylist

loop:
		for {
			select {
			case <-p.Done:
				close(p.Status)
				break loop

			default:
				req, _ = http.NewRequest("GET", p.Variant.URI, nil)
				resp, err = http.DefaultClient.Do(req)

				if err != nil || (resp != nil && resp.StatusCode != 200) {
					p.Status <- fmt.Sprintf("Got a response from VARIANT: %s", resp.Status)
					time.Sleep(5 * time.Second)
					continue
				}

				dir = path.Dir(variant_url.Path)

				playlist, _, _ = m3u8.DecodeFrom(resp.Body, true)
				media_playlist = playlist.(*m3u8.MediaPlaylist)

				resp.Body.Close()

				for _, segment := range media_playlist.Segments {
					if segment != nil {
						_, hit := cache.Get(segment.URI)

						if !hit {
							p.Output <- fmt.Sprintf("%s://%s%s/%s", variant_url.Scheme, variant_url.Host, dir, segment.URI)
							cache.Add(segment.URI, nil)

							p.Status <- fmt.Sprintf("Queueing %s for downloading.", segment.URI)
						}
					}
				}

				time.Sleep(time.Duration(media_playlist.TargetDuration) * time.Second)
			}
		}
	}()
}
