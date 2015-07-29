package streaming

import (
	"encoding/json"
	"fmt"
	"github.com/golang/groupcache/lru"
	"github.com/kz26/m3u8"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	USHER_API_MASK = "http://usher.twitch.tv/api/channel/hls/%s.m3u8?player=twitchweb&token=%s&sig=%s&$allow_audio_only=true&allow_source=true&type=any&p=%d"
	TOKEN_API_MASK = "http://api.twitch.tv/api/channels/%s/access_token"
)

type Playlister struct {
	ChannelName    string
	DesiredVariant string

	Token     string
	Signature string

	Output chan string
	Status chan Status
	Done   chan bool
}

func NewPlaylister(channelName, variant string) *Playlister {
	p := &Playlister{
		ChannelName:    channelName,
		DesiredVariant: variant,

		Output: make(chan string, 1024),
		Status: make(chan Status),
		Done:   make(chan bool),
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
		return fmt.Errorf("[pl] Non-200 code returned for TOKEN: %s", resp.StatusCode)
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

func (p *Playlister) getVariant() (*m3u8.Variant, error) {
	base_url, _ := url.Parse(fmt.Sprintf(USHER_API_MASK, p.ChannelName, p.Token, p.Signature, 123456))
	req, _ := http.NewRequest("GET", base_url.String(), nil)

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return nil, err
	}

	if resp != nil && resp.StatusCode != 200 {
		return nil, fmt.Errorf("[pl] Got a response from USHER: %s", resp.Status)
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

	log.Printf("[pl] Variants found: %s", strings.Join(variants, " "))

	if target_variant == nil {
		return nil, fmt.Errorf("[pl] Variant specified not found for this stream.")
	}

	log.Printf("[pl] Loading variant: %s", p.DesiredVariant)

	return target_variant, nil
}

func (p *Playlister) run() {
	go func() {
		var variant *m3u8.Variant
		var variant_url *url.URL

		err := p.getToken()

		if err != nil {
			close(p.Status)
			return
		}

		variant, err = p.getVariant()

		if err != nil {
			close(p.Status)
			return
		}

		variant_url, _ = url.Parse(variant.URI)
		cache := lru.New(1024)

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
				req, _ = http.NewRequest("GET", variant.URI, nil)
				resp, err = http.DefaultClient.Do(req)

				if err != nil || (resp != nil && resp.StatusCode != 200) {
					log.Printf("[pl] Got a response from VARIANT: %s", resp.Status)
					log.Printf("[pl] Attempting to get new variant location.")

					variant, err = p.getVariant()

					if err != nil {
						log.Printf("[pl] Could not get new variant location: %s", err)
						close(p.Status)
						break loop
					}

					variant_url, _ = url.Parse(variant.URI)
					log.Printf("[pl] Success. New variant location found: %s", variant_url)
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

							// p.Status <- fmt.Sprintf("[pl] Queueing %s for downloading.", segment.URI)
						}
					}
				}

				time.Sleep(time.Duration(media_playlist.TargetDuration) * time.Second)
			}
		}
	}()
}
