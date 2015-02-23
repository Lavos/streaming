package streaming

type TokenResponse struct {
	Token     string `json:"token"`
	Signature string `json:"sig"`
}
