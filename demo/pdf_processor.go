package demo

import (
	"context"
	"os"

	gapi "github.com/glassbox-go/api"
)

//gobox:sandbox
type PDFProcessor interface {
	ExtractTextFromFile(ctx context.Context, path string) (string, error)
}

// PDFProcessorImpl extracts and processes contents of local files.
type PDFProcessorImpl struct{}

func (p *PDFProcessorImpl) ExtractTextFromFile(ctx context.Context, path string) (string, error) {
	// Check context cancellation/timeout early
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Assert filesystem capability boundaries before reading
	gate := &gapi.SecurityGate{}
	if err := gate.CheckFileAccess(ctx, path); err != nil {
		return "", err
	}

	// Read and extract content, validating filesystem isolation
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}
