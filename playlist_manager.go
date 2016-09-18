package streaming

import (
	"encoding/json"
	"fmt"
	"github.com/golang/groupcache/lru"
	"github.com/kz26/m3u8"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"os"
	"io"
	"io/ioutil"
)

const (
	USHER_API_MASK = "http://usher.twitch.tv/api/channel/hls/%s.m3u8?player=twitchweb&token=%s&sig=%s&$allow_audio_only=true&allow_source=true&type=any&p=%d"
	TOKEN_API_MASK = "http://api.twitch.tv/api/channels/%s/access_token"
)

type Configuration struct {
	ClientID string
	Verbose bool
}

type PlaylistManager struct {
	conf Configuration
	loggerVerbose *log.Logger
	loggerStandard *log.Logger

	ChannelName    string
	DesiredVariant string

	Token     string
	Signature string

	outputChan chan string
	statusChan chan Status
	doneChan   chan bool
}

func NewPlaylister(c Configuration, channelName, variant string, out io.Writer) Playlister {
	lv := log.New(ioutil.Discard, "[pl]", log.LstdFlags)
	ls := log.New(os.Stderr, "[pl]", log.LstdFlags)

	if c.Verbose {
		lv.SetOutput(os.Stderr)
	}

	p := &PlaylistManager{
		conf: c,
		loggerVerbose: lv,
		loggerStandard: ls,

		ChannelName:    channelName,
		DesiredVariant: variant,

		outputChan: make(chan string, 1024),
		statusChan: make(chan Status),
		doneChan:   make(chan bool),
	}

	NewDownloader(p.outputChan, p.statusChan, out, lv)
	p.run()

	return p
}

func (p *PlaylistManager) Status() chan Status {
	return p.statusChan
}

func (p *PlaylistManager) Done() {
	p.doneChan <- true
}

func (p *PlaylistManager) getToken() error {
	req, _ := http.NewRequest("GET", fmt.Sprintf(TOKEN_API_MASK, p.ChannelName), nil)
	req.Header.Add("Client-ID", p.conf.ClientID)

	p.loggerVerbose.Printf("Get Token Request: %#v %s", req.Header, req.URL)

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
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

	p.loggerVerbose.Printf("Token: %s", t.Token)
	p.loggerVerbose.Printf("Signature: %s", t.Signature)

	return nil
}

func (p *PlaylistManager) getVariant() (*m3u8.Variant, error) {
	base_url, _ := url.Parse(fmt.Sprintf(USHER_API_MASK, p.ChannelName, p.Token, p.Signature, 123456))
	req, _ := http.NewRequest("GET", base_url.String(), nil)

	p.loggerVerbose.Printf("Get Variant Request: %#v %#v", req.Header, req.URL)

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
			p.loggerVerbose.Printf("Variant: %#v", variant)
			variants[i] = variant.Video

			if p.DesiredVariant == variant.Video {
				target_variant = variant
			}
		}
	}

	p.loggerStandard.Printf("Variants found: %s", strings.Join(variants, " "))

	if target_variant == nil {
		return nil, fmt.Errorf("Variant specified not found for this stream.")
	}

	p.loggerStandard.Printf("Loading variant: %s", p.DesiredVariant)

	return target_variant, nil
}

func (p *PlaylistManager) run() {
	go func() {
		var variant *m3u8.Variant
		var variant_url *url.URL

		p.loggerVerbose.Printf("Getting Token.")
		err := p.getToken()

		if err != nil {
			p.loggerStandard.Printf("Token Error: %s", err)
			p.stop()
			return
		}

		cache := lru.New(1024)

		var req *http.Request
		var resp *http.Response
		var playlist m3u8.Playlist
		var media_playlist *m3u8.MediaPlaylist


		// first, get variant URL
		var variantcounter int

		for {
			if variantcounter == 5 {
				p.loggerStandard.Printf("Could not get variant URL. Aborting.")
				p.stop()
				return
			}

			variant, err = p.getVariant()

			if err != nil {
				p.loggerVerbose.Printf("[%d] Could not get variant: %s", variantcounter, err)
				variantcounter++
				continue
			}

			break
		}

	loop:
		for {
			select {
			case <-p.doneChan:
				p.stop()
				break loop

			default:
				var retrycounter int

				for {
					if retrycounter == 5 {
						p.loggerStandard.Printf("Could not get Playlist. Aborting.")
						p.stop()
						break loop
					}

					// second, try to get playlist from body
					req, _ = http.NewRequest("GET", variant.URI, nil)
					resp, err = http.DefaultClient.Do(req)

					if err != nil || (resp != nil && resp.StatusCode != http.StatusOK) {
						if err != nil {
							p.loggerVerbose.Printf("[%d] Got an ERROR from VARIANT: %s", retrycounter, err)
							p.loggerVerbose.Printf("[%d] Attempting to get new variant location.", retrycounter)
						}

						if resp != nil {
							p.loggerVerbose.Printf("[%d] Got a response from VARIANT: %s", retrycounter, resp.Status)
							p.loggerVerbose.Printf("[%d] Attempting to get new variant location.", retrycounter)
						}


						variantcounter = 0

						for {
							if variantcounter == 5 {
								p.loggerStandard.Printf("Could not get variant URL. Aborting.")
								p.stop()
								return
							}

							variant, err = p.getVariant()

							if err != nil {
								p.loggerVerbose.Printf("[%d] Could not get variant: %s", variantcounter, err)
								time.Sleep(1 * time.Second)
								variantcounter++
								continue
							}

							break
						}
					}

					p.loggerVerbose.Printf("[%d] Success. New variant location found: %s", retrycounter, variant_url)
					variant_url, _ = url.Parse(variant.URI)
					break
				}

				p.loggerVerbose.Printf("Variant URL: %#v", variant_url)

				playlist, _, _ = m3u8.DecodeFrom(resp.Body, true)
				media_playlist = playlist.(*m3u8.MediaPlaylist)

				resp.Body.Close()

				for _, segment := range media_playlist.Segments {
					if segment != nil {
						p.loggerVerbose.Printf("Segment: %#v", segment)

						_, hit := cache.Get(segment.URI)

						if !hit {
							p.loggerVerbose.Printf("Queueing %s for downloading.", segment.URI)
							p.outputChan <- segment.URI
							cache.Add(segment.URI, nil)
						}
					}
				}

				time.Sleep(time.Duration(media_playlist.TargetDuration) * time.Second)
			}
		}
	}()
}

func (p *PlaylistManager) stop() {
	close(p.statusChan)
	close(p.outputChan)
}