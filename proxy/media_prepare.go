package proxy

import (
	"context"
	"encoding/base64"
	"strings"
)

// Media preparation runs once per request, before translation. It downloads
// URL-referenced images and documents (image_fetch.go), rewrites them into
// the inline base64 shape the translators read, and validates every document
// (format, size, count). Any problem is returned as a *mediaRequestError,
// which the handlers answer with 400 invalid_request_error; translation itself
// never has to fail or drop content.

// hasMediaParts reports whether a message carries image or document parts in
// any API's shape. Request-shape validation counts these as user content even
// before URL sources are downloaded.
func hasMediaParts(content interface{}) bool {
	var parts []interface{}
	switch c := content.(type) {
	case []interface{}:
		parts = c
	case []map[string]interface{}:
		for _, m := range c {
			parts = append(parts, m)
		}
	case map[string]interface{}:
		parts = []interface{}{c}
	}
	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		switch t, _ := part["type"].(string); t {
		case "image", "document", "image_url", "input_image", "file", "input_file":
			return true
		}
	}
	return false
}

// mediaErrorStatus maps a preparation error to an HTTP status and message:
// client problems are 400, anything else (e.g. a broken proxy setting) 500.
func mediaErrorStatus(err error) (int, string, string) {
	if me, ok := asMediaError(err); ok {
		return 400, "invalid_request_error", me.msg
	}
	return 500, "api_error", "could not prepare request media: " + err.Error()
}

// mediaBudget tracks per-request limits while walking the messages.
//
// The document limit applies per message. Clients resend the whole history on
// every turn, so a request-wide cap would make a conversation fail forever once
// it had accumulated more documents; instead finalizePayloadDocuments keeps the
// newest documents and replaces older ones in history with a note.
type mediaBudget struct {
	ctx       context.Context
	documents int // documents in the message being walked
	fetches   int
}

func (b *mediaBudget) countDocument() error {
	b.documents++
	if b.documents > maxDocumentsPerRequest {
		return newMediaError("too many documents: at most %d documents are supported per message", maxDocumentsPerRequest)
	}
	return nil
}

// nextMessage resets the per-message document count.
func (b *mediaBudget) nextMessage() { b.documents = 0 }

func (b *mediaBudget) fetch(rawURL string, kind remoteMediaKind, maxBytes int64) (*fetchedMedia, error) {
	b.fetches++
	if b.fetches > maxRemoteMediaPerRequest {
		return nil, newMediaError("too many media URLs: at most %d are fetched per request", maxRemoteMediaPerRequest)
	}
	return fetchRemoteMedia(b.ctx, rawURL, kind, maxBytes)
}

// ---- Anthropic ----

// prepareClaudeMedia resolves URL sources and validates documents in place.
func prepareClaudeMedia(ctx context.Context, req *ClaudeRequest) error {
	if req == nil {
		return nil
	}
	b := &mediaBudget{ctx: ctx}
	for i := range req.Messages {
		if req.Messages[i].Role != "user" {
			// thinking / redacted_thinking and other assistant-only blocks are
			// handled (dropped) by the assistant extractor.
			continue
		}
		b.nextMessage()
		if err := b.prepareClaudeBlocks(req.Messages[i].Content); err != nil {
			return err
		}
	}
	return nil
}

func (b *mediaBudget) prepareClaudeBlocks(content interface{}) error {
	for _, block := range contentBlocksAsMaps(content) {
		switch t, _ := block["type"].(string); t {
		case "image":
			if err := b.prepareClaudeImage(block); err != nil {
				return err
			}
		case "document":
			if err := b.prepareClaudeDocument(block); err != nil {
				return err
			}
		case "tool_result":
			if err := b.prepareClaudeBlocks(block["content"]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *mediaBudget) prepareClaudeImage(block map[string]interface{}) error {
	source, _ := block["source"].(map[string]interface{})
	if source == nil {
		return nil
	}
	srcType, _ := source["type"].(string)
	rawURL, _ := source["url"].(string)
	switch {
	case strings.EqualFold(srcType, "url") || (rawURL != "" && isRemoteURL(rawURL)):
		if rawURL == "" {
			return newMediaError("image source of type url has no url")
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "data:") {
			return nil // a data URL in a url source is read inline
		}
		if !isRemoteURL(rawURL) {
			return newMediaError("image url must be an https URL or a data URL")
		}
		media, err := b.fetch(rawURL, remoteImage, maxFetchedImageBytes)
		if err != nil {
			return err
		}
		block["source"] = map[string]interface{}{
			"type":       "base64",
			"media_type": media.MediaType,
			"data":       base64.StdEncoding.EncodeToString(media.Data),
		}
	case strings.EqualFold(srcType, "file"):
		return newMediaError("image source of type file (file_id) is not supported; send the image as base64 data")
	}
	return nil
}

func (b *mediaBudget) prepareClaudeDocument(block map[string]interface{}) error {
	source, _ := block["source"].(map[string]interface{})
	if source != nil {
		if srcType, _ := source["type"].(string); strings.EqualFold(srcType, "url") {
			rawURL, _ := source["url"].(string)
			if rawURL == "" {
				return newMediaError("document source of type url has no url")
			}
			media, err := b.fetch(rawURL, remoteDocument, maxDocumentBytes)
			if err != nil {
				return err
			}
			newSource := map[string]interface{}{
				"type":       "base64",
				"media_type": media.MediaType,
				"data":       base64.StdEncoding.EncodeToString(media.Data),
			}
			if claudeDocumentTitle(block) == "" && media.FileName != "" {
				block["title"] = media.FileName
			}
			block["source"] = newSource
		}
	}
	doc, _, _, err := claudeDocumentParts(block)
	if err != nil {
		return err
	}
	if doc != nil {
		return b.countDocument()
	}
	return nil
}

// ---- OpenAI Chat / Responses ----

// prepareOpenAIMedia resolves image and file URLs and validates file parts of
// OpenAI-shaped messages (Chat Completions, and Responses input after
// conversion).
func prepareOpenAIMedia(ctx context.Context, messages []OpenAIMessage) error {
	b := &mediaBudget{ctx: ctx}
	for i := range messages {
		role := messages[i].Role
		if role != "user" && role != "tool" {
			continue
		}
		b.nextMessage()
		if err := b.prepareOpenAIContent(messages[i].Content); err != nil {
			return err
		}
	}
	return nil
}

func (b *mediaBudget) prepareOpenAIContent(content interface{}) error {
	var parts []interface{}
	switch c := content.(type) {
	case []interface{}:
		parts = c
	case map[string]interface{}:
		parts = []interface{}{c}
	default:
		return nil
	}
	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		t, _ := part["type"].(string)
		switch t {
		case "image_url", "input_image", "image":
			if err := b.prepareOpenAIImage(part); err != nil {
				return err
			}
		case "file", "input_file":
			if err := b.prepareOpenAIFile(part); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *mediaBudget) fetchImageDataURL(rawURL string) (string, error) {
	media, err := b.fetch(rawURL, remoteImage, maxFetchedImageBytes)
	if err != nil {
		return "", err
	}
	return "data:" + media.MediaType + ";base64," + base64.StdEncoding.EncodeToString(media.Data), nil
}

func (b *mediaBudget) prepareOpenAIImage(part map[string]interface{}) error {
	switch v := part["image_url"].(type) {
	case string:
		if isRemoteURL(v) {
			data, err := b.fetchImageDataURL(v)
			if err != nil {
				return err
			}
			part["image_url"] = data
		} else if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "data:") && strings.TrimSpace(v) != "" {
			return newMediaError("image_url must be an https URL or a data URL")
		}
	case map[string]interface{}:
		if u, _ := v["url"].(string); isRemoteURL(u) {
			data, err := b.fetchImageDataURL(u)
			if err != nil {
				return err
			}
			v["url"] = data
		} else if strings.TrimSpace(u) != "" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(u)), "data:") {
			return newMediaError("image_url.url must be an https URL or a data URL")
		}
	case nil:
		if id, _ := part["file_id"].(string); id != "" {
			return newMediaError("input_image with file_id is not supported; send the image as a data URL or https URL")
		}
	}
	return nil
}

func (b *mediaBudget) prepareOpenAIFile(part map[string]interface{}) error {
	filename, fileData, _, fileURL := openAIFileFields(part)
	if fileData == "" && fileURL != "" {
		media, err := b.fetch(fileURL, remoteDocument, maxDocumentBytes)
		if err != nil {
			return err
		}
		obj := part
		if f, ok := part["file"].(map[string]interface{}); ok {
			obj = f
		}
		obj["file_data"] = "data:" + media.MediaType + ";base64," + base64.StdEncoding.EncodeToString(media.Data)
		if filename == "" && media.FileName != "" {
			obj["filename"] = media.FileName
		}
		delete(obj, "file_url")
	}
	if extractImageFromOpenAIPart(part) != nil {
		return nil
	}
	doc, _, err := openAIFilePart(part)
	if err != nil {
		return err
	}
	if doc != nil {
		return b.countDocument()
	}
	return nil
}
