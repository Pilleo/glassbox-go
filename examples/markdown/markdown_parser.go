package markdown

import (
	"bytes"
	"context"

	"github.com/yuin/goldmark"
)

//gobox:sandbox
type MarkdownParser interface {
	Render(ctx context.Context, markdown []byte) (string, error)
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
