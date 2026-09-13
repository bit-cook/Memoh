//go:build !linux || (!amd64 && !arm64)

package vision

import "context"

func renderLottie(_ context.Context, _ []byte) ([]Frame, error) {
	return nil, ErrTGSUnavailable
}
