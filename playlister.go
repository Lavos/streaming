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

	if resp.StatusCode != http.StatusOK {
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

	if resp != nil && resp.StatusCode != http.StatusOK {
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

		cache := lru.New(1024)

		var req *http.Request
		var resp *http.Response
		var dir string
		var playlist m3u8.Playlist
		var media_playlist *m3u8.MediaPlaylist


		// first, get variant URL
		var variantcounter int

		for {
			if variantcounter == 5 {
				log.Printf("Could not get variant URL. Aborting.")
				close(p.Status)
				return
			}

			variant, err = p.getVariant()

			if err != nil {
				log.Printf("[pl][%d] Could not get variant: %s", variantcounter, err)
				variantcounter++
				continue
			}

			break
		}

	loop:
		for {
			select {
			case <-p.Done:
				close(p.Status)
				break loop

			default:
				var retrycounter int

				for {
					if retrycounter == 5 {
						log.Printf("[pl] Retries exhausted. Closing.")
						close(p.Status)
						break loop
					}

					// second, try to get playlist from body
					req, _ = http.NewRequest("GET", variant.URI, nil)
					resp, err = http.DefaultClient.Do(req)

					if err != nil || (resp != nil && resp.StatusCode != http.StatusOK) {
						if err != nil {
							log.Printf("[pl][%d] Got an ERROR from VARIANT: %s", retrycounter, err)
							log.Printf("[pl][%d] Attempting to get new variant location.", retrycounter)
						}

						if resp != nil {
							log.Printf("[pl][%d] Got a response from VARIANT: %s", retrycounter, resp.Status)
							log.Printf("[pl][%d] Attempting to get new variant location.", retrycounter)
						}


						variantcounter = 0

						for {
							if variantcounter == 5 {
								log.Printf("Could not get variant URL. Aborting.")
								close(p.Status)
								return
							}

							variant, err = p.getVariant()

							if err != nil {
								log.Printf("[pl][%d] Could not get variant: %s", variantcounter, err)
								time.Sleep(1 * time.Second)
								variantcounter++
								continue
							}

							break
						}
					}

					// log.Printf("[pl][%d] Success. New variant location found: %s", retrycounter, variant_url)
					variant_url, _ = url.Parse(variant.URI)
					break
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
