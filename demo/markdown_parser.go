package demo

import (
	"bytes"
	"context"

	gapi "github.com/glassbox-go/api"
	"github.com/yuin/goldmark"
)

//gobox:sandbox
type MarkdownParser interface {
	Render(ctx context.Context, markdown []byte) (string, error)
	RenderWithTemplate(ctx context.Context, markdown []byte, templateUrl string) (string, error)
}

// MarkdownParserImpl handles parsing and rendering of Markdown documents to HTML.
type MarkdownParserImpl struct{}

func (m *MarkdownParserImpl) Render(ctx context.Context, markdown []byte) (string, error) {
	var buf bytes.Buffer
	err := goldmark.Convert(markdown, &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *MarkdownParserImpl) RenderWithTemplate(ctx context.Context, markdown []byte, templateUrl string) (string, error) {
	// Securely fetch styling template via egress-filtered VirtualHTTPClient
	client := gapi.NewVirtualHTTPClient()
	template, err := client.Fetch(ctx, templateUrl)
	if err != nil {
		return "", err
	}

	rendered, err := m.Render(ctx, markdown)
	if err != nil {
		return "", err
	}

	return template + "\n" + rendered, nil
}
