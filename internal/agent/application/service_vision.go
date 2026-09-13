package application

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/media/vision"
)

func (s *Service) prepareStoredVision(ctx context.Context, botID, hash string, sticker bool) ([]vision.Frame, error) {
	if s == nil || s.assetLoader == nil {
		return nil, errors.New("media loader unavailable")
	}
	reader, _, err := s.assetLoader.OpenForGateway(ctx, botID, hash)
	if err != nil {
		return nil, errors.New("stored image could not be opened")
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, vision.MaxBytes+1))
	if err != nil {
		return nil, errors.New("stored image could not be read")
	}
	return s.prepareVisionBytes(ctx, botID, data, sticker)
}

func (s *Service) prepareVisionBytes(ctx context.Context, botID string, data []byte, sticker bool) ([]vision.Frame, error) {
	s.visionOnce.Do(func() { s.visionProcessor = vision.NewProcessor() })
	return s.visionProcessor.Prepare(ctx, botID, data, sticker)
}

func visionImageParts(frames []vision.Frame) []sdk.ImagePart {
	out := make([]sdk.ImagePart, 0, len(frames))
	for _, f := range frames {
		out = append(out, sdk.ImagePart{Image: "data:" + f.MIME + ";base64," + base64.StdEncoding.EncodeToString(f.Data), MediaType: f.MIME})
	}
	return out
}

func (s *Service) logVisionFailure(err error) {
	if s != nil && s.logger != nil {
		s.logger.Warn("media omitted from vision input; original attachment retained", slog.Any("error", err))
	}
}

// prepareVisionAttachments expands animation frames before capability and size
// routing. A malformed or unsupported media input becomes a file reference;
// it must not poison the turn or escape via its original image URL.
func (s *Service) prepareVisionAttachments(ctx context.Context, botID string, item gatewayAttachment) []gatewayAttachment {
	if item.ContentHash == "" && item.Transport == gatewayTransportPublicURL && item.Type == "image" && isNativeImageAttachment(item) {
		return []gatewayAttachment{item}
	}
	var frames []vision.Frame
	var err error
	switch {
	case item.ContentHash != "":
		frames, err = s.prepareStoredVision(ctx, botID, item.ContentHash, item.Type == "sticker")
	case item.Transport == gatewayTransportInlineDataURL:
		payload, _ := splitInlineDataURL(item.Payload)
		if len(payload) > ((vision.MaxBytes+2)/3)*4 {
			err = errors.New("inline image exceeds preparation limit")
		} else {
			var data []byte
			data, err = base64.StdEncoding.DecodeString(payload)
			if err == nil {
				frames, err = s.prepareVisionBytes(ctx, botID, data, item.Type == "sticker")
			} else {
				err = errors.New("invalid image encoding")
			}
		}
	default:
		err = errors.New("sticker has no stored media or inline bytes")
	}
	if err != nil {
		s.logVisionFailure(err)
		item.invalidInlineImage = item.ContentHash == "" && item.Transport == gatewayTransportInlineDataURL
		item.Type = "file"
		item.Transport = gatewayTransportToolFileRef
		item.Payload = strings.TrimSpace(item.FallbackPath)
		// Force an opaque file fallback even when the original MIME claimed to be
		// readable text. Keep the original MIME only in the stored media record.
		item.Mime = "application/octet-stream"
		return []gatewayAttachment{item}
	}
	out := make([]gatewayAttachment, 0, len(frames))
	for _, f := range frames {
		frame := item
		frame.Type = "image"
		frame.Mime = f.MIME
		frame.Size = int64(len(f.Data))
		frame.Transport = gatewayTransportInlineDataURL
		frame.Payload = "data:" + f.MIME + ";base64," + base64.StdEncoding.EncodeToString(f.Data)
		out = append(out, frame)
	}
	return out
}
