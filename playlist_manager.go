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
	"path"
	"io/ioutil"
	"context"

	"golang.org/x/oauth2"
)

const (
	USHER_API_MASK = "http://usher.twitch.tv/api/channel/hls/%s.m3u8"
	TOKEN_API_MASK = "https://api.twitch.tv/api/channels/%s/access_token"
	TOKEN_ENDPOINT = "https://id.twitch.tv/oauth2/token"
)

var (
	Client *http.Client = http.DefaultClient
)

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

	tokensrc oauth2.TokenSource
}

func NewPlaylister(c Configuration, channelName, variant string, out io.Writer) Playlister {
	lv := log.New(ioutil.Discard, "[pl]", log.LstdFlags)
	ls := log.New(os.Stderr, "[pl]", log.LstdFlags)

	if c.Verbose {
		lv.SetOutput(os.Stderr)
		lv.Printf("Logging verbosely.")
		lv.Printf("Using configuration: %#v", c)
	}

	p := &PlaylistManager{
		conf: c,
		loggerVerbose: lv,
		loggerStandard: ls,

		ChannelName:    channelName,
		DesiredVariant: variant,

		outputChan: make(chan string, 1024),
		statusChan: make(chan Status, 1024),
		doneChan:   make(chan bool),
	}

	if c.Authenticate && c.RefreshToken != "" {
		oc := &oauth2.Config{
			ClientID: c.ClientID,
			ClientSecret: c.ClientSecret,
			Scopes: []string{},
			Endpoint: oauth2.Endpoint{
				TokenURL: TOKEN_ENDPOINT,
			},
			RedirectURL: c.RedirectURI,
		}

		p.tokensrc = oc.TokenSource(context.Background(), &oauth2.Token{
			RefreshToken: c.RefreshToken,
		})
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
	u, _ := url.Parse(fmt.Sprintf(TOKEN_API_MASK, p.ChannelName))
	v := url.Values{}
	v.Add("need_https", "false")
	v.Add("adblock", "false")
	v.Add("platform", "web")
	v.Add("player_backend", "mediaplayer")
	v.Add("player_type", "site")

	req, _ := http.NewRequest("GET", u.String(), nil)

	if p.tokensrc != nil {
		req.Header.Add("Client-ID", p.conf.ClientID)
		token, err := p.tokensrc.Token()

		if err != nil {
			p.loggerStandard.Printf("Could not get Oauth2 Token: %s - stream viewership will be anonymous.", err)
		} else {
			p.loggerVerbose.Printf("Oauth2 Token: %#v", token)
			p.loggerStandard.Printf("Stream viewership is authenticated.")
			v.Add("oauth_token", token.AccessToken)
		}
	} else if p.conf.OAuth2Token != "" {
		req.Header.Add("Client-ID", "kimne78kx3ncx6brgo4mv6wki5h1ko")
		req.Header.Add("Authorization", fmt.Sprintf("OAuth %s", p.conf.OAuth2Token))
		p.loggerStandard.Printf("Stream viewership is authenticated using Twitch's ClientID.")
	} else {

		p.loggerStandard.Printf("Stream viewership is anonymous.")
	}

	u.RawQuery = v.Encode()

	p.loggerVerbose.Printf("Get Token Request: %#v %s", req.Header, req.URL)

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Non-200 code returned for TOKEN: %s", resp.Status)
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
	base_url, _ := url.Parse(fmt.Sprintf(USHER_API_MASK, p.ChannelName))

	v := url.Values{}
	v.Add("player_backend", "html5")
	v.Add("token", p.Token)
	v.Add("sig", p.Signature)
	v.Add("allow_audio_only", "true")
	v.Add("allow_source", "true")
	v.Add("allow_spectre", "false")
	v.Add("type", "any")
	v.Add("p", "123456")

	base_url.RawQuery = v.Encode()
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
		var dir string
		var segment_url string
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

		var new_segments []string
		var foundEdge bool
		var leadTime time.Time
		var leadDuration time.Duration

loop:
		for {
			leadTime = time.Now()

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
				p.loggerVerbose.Printf("Segments found: %d", len(media_playlist.Segments))

				new_segments = make([]string, 0)

				for _, segment := range media_playlist.Segments {
					if segment != nil {
						p.loggerVerbose.Printf("Segment: %#v", segment)

						// check if it's a relative or absolute URL
						if strings.HasPrefix(segment.URI, "http") {
							segment_url = segment.URI
						} else {
							dir = path.Dir(variant_url.Path)
							segment_url = fmt.Sprintf("%s://%s%s/%s", variant_url.Scheme, variant_url.Host, dir, segment.URI)
						}

						_, hit := cache.Get(segment_url)

						if !hit {
							cache.Add(segment_url, nil)
							new_segments = append(new_segments, segment_url)
						}
					}
				}

				p.loggerVerbose.Printf("Found %d new segments.", len(new_segments))

				if len(new_segments) == 0 {
					p.loggerVerbose.Printf("No Segments Found in %s", time.Now().Sub(leadTime))
					leadDuration = (time.Duration(media_playlist.TargetDuration / 2) * time.Second) - time.Now().Sub(leadTime)
					p.loggerVerbose.Printf("Sleeping for %s", leadDuration)
					time.Sleep(leadDuration)
					continue loop
				}

				if !foundEdge {
					foundEdge = true

					if len(new_segments) > 3 {
						p.outputChan <- new_segments[len(new_segments) - 4]
						p.outputChan <- new_segments[len(new_segments) - 3]
						p.outputChan <- new_segments[len(new_segments) - 2]
						p.outputChan <- new_segments[len(new_segments) - 1]
					}
				} else {
					for _, url := range new_segments {
						p.loggerVerbose.Printf("Queueing %s for downloading.", url)
						p.outputChan <- url
					}
				}

				p.loggerVerbose.Printf("Segments Found in %s", time.Now().Sub(leadTime))
				leadDuration = (time.Duration(media_playlist.TargetDuration) * time.Second) - time.Now().Sub(leadTime)
				p.loggerVerbose.Printf("Sleeping for %s", leadDuration)
				time.Sleep(leadDuration)
				continue loop
			}
		}
	}()
}

func (p *PlaylistManager) stop() {
	close(p.statusChan)
	close(p.outputChan)
}
