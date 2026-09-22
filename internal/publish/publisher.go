// Package publish sends finished posts to publishing targets.
package publish

import "context"

// Channel is how a target is reached.
type Channel string

const (
	// ChannelAPI uses a platform's official API.
	ChannelAPI Channel = "api"
	// ChannelExtension drives the platform's own editor through the
	// companion browser extension, for platforms with no write API.
	ChannelExtension Channel = "extension"
)

// Caps describes what a target accepts, so the UI can ask for what is
// required rather than failing at submit time.
type Caps struct {
	Channel       Channel `json:"channel"`
	DraftOnly     bool    `json:"draftOnly"`
	RequiresCover bool    `json:"requiresCover"`
	LinksAllowed  bool    `json:"linksAllowed"`
	TitleLimit    int     `json:"titleLimit"`
	DigestLimit   int     `json:"digestLimit"`
}

// Article is a rendered post ready to submit.
type Article struct {
	Title   string
	Author  string
	Digest  string
	HTML    string
	Cover   []byte // raw image bytes, if the target needs one
	CoverCT string // cover content type
	Images  []Image
}

// Image is a local image referenced by the article body.
type Image struct {
	// SrcURL is how the image appears in the rendered HTML; it is
	// rewritten to the platform's own URL before submitting.
	SrcURL string
	Data   []byte
	CT     string
}

// Result is what a successful publish produced.
type Result struct {
	RemoteID  string `json:"remoteId"`
	RemoteURL string `json:"remoteUrl"`
	Status    string `json:"status"`
}

// Publisher is one publishing target.
type Publisher interface {
	Platform() string
	Caps() Caps
	// Validate checks credentials and connectivity without publishing.
	Validate(ctx context.Context) error
	Publish(ctx context.Context, a Article) (*Result, error)
}
