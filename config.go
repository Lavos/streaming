package streaming

type Configuration struct {
	ClientID string
	ClientSecret string
	RedirectURI string `default:"http://localhost"`
	RefreshToken string
	Verbose bool

	Authenticate bool `default:"false"`
	OAuth2Token string
}
