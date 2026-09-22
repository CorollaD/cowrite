package wechat

import (
	"context"
	"fmt"
	"strings"

	"github.com/corollad/cowrite/internal/publish"
)

// Publisher submits articles to the WeChat draft box.
//
// It stops at the draft rather than broadcasting: sending to subscribers
// is irreversible, so the last step stays a human click in WeChat's own
// console.
type Publisher struct {
	client *Client
	author string
}

func NewPublisher(c *Client, author string) *Publisher {
	return &Publisher{client: c, author: author}
}

func (p *Publisher) Platform() string { return "wechat" }

func (p *Publisher) Caps() publish.Caps {
	return publish.Caps{
		Channel:       publish.ChannelAPI,
		DraftOnly:     true,
		RequiresCover: true,
		LinksAllowed:  false, // stripped for unverified accounts
		TitleLimit:    TitleLimit,
		DigestLimit:   DigestLimit,
	}
}

func (p *Publisher) Validate(ctx context.Context) error {
	return p.client.Validate(ctx)
}

// Publish uploads the images, rewrites the body to point at them, and
// creates the draft.
func (p *Publisher) Publish(ctx context.Context, a publish.Article) (*publish.Result, error) {
	title := TrimTitle(a.Title)
	if title == "" {
		return nil, fmt.Errorf("标题不能为空")
	}
	// WeChat rejects a draft with no cover, but only after the body images
	// have been uploaded. Failing here keeps the error understandable and
	// avoids spending the material quota on a submission that cannot land.
	if len(a.Cover) == 0 {
		return nil, fmt.Errorf("公众号草稿必须有封面图：在文章 front matter 里加一行 cover: 图片文件名（图片放在文章同目录）")
	}

	html := a.HTML
	// Body images must live on mmbiz.qpic.cn; anything else is stripped by
	// the editor, leaving the article with broken images.
	for i, img := range a.Images {
		if len(img.Data) == 0 {
			continue
		}
		url, err := p.client.UploadImage(ctx, img.Data, fmt.Sprintf("image-%d%s", i, extFor(img.CT)), img.CT)
		if err != nil {
			return nil, fmt.Errorf("上传正文图片失败: %w", err)
		}
		html = strings.ReplaceAll(html, img.SrcURL, url)
	}

	var thumbID string
	if len(a.Cover) > 0 {
		id, err := p.client.UploadThumb(ctx, a.Cover, "cover"+extFor(a.CoverCT), a.CoverCT)
		if err != nil {
			return nil, fmt.Errorf("上传封面失败: %w", err)
		}
		thumbID = id
	}

	author := a.Author
	if author == "" {
		author = p.author
	}

	mediaID, err := p.client.AddDraft(ctx, []DraftArticle{{
		ArticleType:  "news",
		Title:        title,
		Author:       author,
		Digest:       TrimDigest(a.Digest),
		Content:      html,
		ThumbMediaID: thumbID,
	}})
	if err != nil {
		return nil, err
	}

	return &publish.Result{
		RemoteID: mediaID,
		// The draft box has no per-draft URL, so this points at the list.
		RemoteURL: "https://mp.weixin.qq.com/cgi-bin/appmsg?t=media/appmsg_edit&action=edit",
		Status:    "draft",
	}, nil
}

func extFor(contentType string) string {
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "gif"):
		return ".gif"
	default:
		return ".jpg"
	}
}
