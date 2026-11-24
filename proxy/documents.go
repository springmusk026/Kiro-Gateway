package proxy

import (
	"encoding/base64"
	"fmt"
	"html"
	"mime"
	"path"
	"regexp"
	"strings"
)

// Documents (PDF, Office files, text) on every API.
//
// Inbound shapes:
//   - Anthropic `document` blocks with a base64, text, content or url source
//     (also inside tool_result content);
//   - OpenAI Chat `file` parts ({file:{filename, file_data:"data:…"}});
//   - Responses `input_file` items and content parts (filename + file_data).
//
// Binary documents become Kiro documents — `documents:[{name, format,
// source:{bytes}}]` on generateAssistantResponse and an `input_file` content
// part on /v1/responses. Anthropic plain-text sources (`text`, `content`) have
// no file behind them and are inlined as text inside a <document name="…">
// wrapper. Every document is either forwarded or rejected with a 400 that
// names the problem; none is dropped silently.

// KiroDocument is a document attached to a Kiro user message.
type KiroDocument struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Source struct {
		Bytes string `json:"bytes"`
	} `json:"source"`
}

const (
	// maxDocumentsPerRequest mirrors the backend's DOCUMENT_COUNT_EXCEEDED
	// validation (Bedrock-style models accept five documents per request).
	maxDocumentsPerRequest = 5
	// maxDocumentBytes is the decoded size limit per document
	// (DOCUMENT_SIZE_EXCEEDED); also applied to inlined text documents.
	maxDocumentBytes = 4718592 // 4.5 MiB
	// maxDocumentNameLen keeps generated names short and valid.
	maxDocumentNameLen = 100

	defaultDocumentName      = "document"
	documentOnlyUserContent  = "Please analyze the attached document."
	toolResultDocumentFormat = "[Tool returned the document %q; it is attached to this message.]"
)

// documentFormatByMIME maps media types to Kiro's DocumentFormat enum.
var documentFormatByMIME = map[string]string{
	"application/pdf":    "pdf",
	"text/csv":           "csv",
	"application/msword": "doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"text/html":                "html",
	"text/markdown":            "md",
	"text/x-markdown":          "md",
	"text/plain":               "txt",
	"application/vnd.ms-excel": "xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "xlsx",
}

// documentMIMEByFormat is the canonical media type per format, used for the
// /v1/responses data URL.
var documentMIMEByFormat = map[string]string{
	"pdf":  "application/pdf",
	"csv":  "text/csv",
	"doc":  "application/msword",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"html": "text/html",
	"md":   "text/markdown",
	"txt":  "text/plain",
	"xls":  "application/vnd.ms-excel",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
}

// documentFormatFor resolves the Kiro format from a media type, falling back
// to the filename extension when the type is missing or generic.
func documentFormatFor(mediaType, filename string) string {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	if parsed, _, err := mime.ParseMediaType(mt); err == nil {
		mt = parsed
	}
	if f, ok := documentFormatByMIME[mt]; ok {
		return f
	}
	if mt == "" || mt == "application/octet-stream" || mt == "binary/octet-stream" {
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(filename)), ".")
		if ext == "htm" {
			ext = "html"
		}
		if ext == "markdown" {
			ext = "md"
		}
		if _, ok := documentMIMEByFormat[ext]; ok {
			return ext
		}
	}
	return ""
}

func supportedDocumentTypesText() string {
	return "pdf, csv, doc, docx, html, md, txt, xls, xlsx"
}

// decodedBase64Len validates base64 data and returns its decoded length.
func decodedBase64Len(data string) (int, bool) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if n, err := enc.DecodeString(data); err == nil {
			return len(n), true
		}
	}
	return 0, false
}

var dataURLPattern = regexp.MustCompile(`(?s)^data:([^;,]*)((?:;[^;,]*)*),(.*)$`)

// parseAnyDataURL splits a data URL into media type and base64 payload.
func parseAnyDataURL(s string) (mediaType, data string, ok bool) {
	cleaned := strings.TrimSpace(strings.NewReplacer("\n", "", "\r", "").Replace(s))
	m := dataURLPattern.FindStringSubmatch(cleaned)
	if m == nil || !strings.Contains(strings.ToLower(m[2]), ";base64") {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(m[1])), m[3], true
}

// newKiroDocument validates and builds a binary document.
func newKiroDocument(name, mediaType, filename, data string) (*KiroDocument, error) {
	label := name
	if label == "" {
		label = filename
	}
	if label == "" {
		label = defaultDocumentName
	}
	format := documentFormatFor(mediaType, filename)
	if format == "" {
		if mediaType == "" {
			return nil, newMediaError("document %q has no media type; supported formats: %s", label, supportedDocumentTypesText())
		}
		return nil, newMediaError("document %q has unsupported type %q; supported formats: %s", label, mediaType, supportedDocumentTypesText())
	}
	n, ok := decodedBase64Len(data)
	if !ok {
		return nil, newMediaError("document %q is not valid base64 data", label)
	}
	if n == 0 {
		return nil, newMediaError("document %q is empty", label)
	}
	if n > maxDocumentBytes {
		return nil, newMediaError("document %q is %d bytes; the limit is %d bytes per document", label, n, maxDocumentBytes)
	}
	if name == "" {
		name = strings.TrimSuffix(filename, path.Ext(filename))
	}
	doc := &KiroDocument{Name: name, Format: format}
	doc.Source.Bytes = data
	return doc, nil
}

// wrapInlineDocument renders a plain-text document for the prompt.
func wrapInlineDocument(name, text string) string {
	if name == "" {
		name = defaultDocumentName
	}
	return fmt.Sprintf("<document name=\"%s\">\n%s\n</document>", html.EscapeString(name), text)
}

func joinPromptText(existing, add string) string {
	if strings.TrimSpace(existing) == "" {
		return add
	}
	if strings.TrimSpace(add) == "" {
		return existing
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + add
}

// ---- Anthropic document blocks ----

// claudeDocumentTitle returns the client-supplied name of a document block.
func claudeDocumentTitle(block map[string]interface{}) string {
	for _, k := range []string{"title", "name", "filename"} {
		if s, ok := block[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// claudeDocumentParts resolves an Anthropic document block into either a Kiro
// document, or inline text (plus images for `content` sources).
func claudeDocumentParts(block map[string]interface{}) (*KiroDocument, string, []KiroImage, error) {
	title := claudeDocumentTitle(block)
	label := title
	if label == "" {
		label = defaultDocumentName
	}
	source, _ := block["source"].(map[string]interface{})
	if source == nil {
		return nil, "", nil, newMediaError("document %q has no source", label)
	}
	srcType, _ := source["type"].(string)
	mediaType, _ := source["media_type"].(string)
	switch strings.ToLower(srcType) {
	case "base64", "":
		data, _ := source["data"].(string)
		if data == "" {
			return nil, "", nil, newMediaError("document %q has no data", label)
		}
		if mt, payload, ok := parseAnyDataURL(data); ok {
			data = payload
			if mediaType == "" {
				mediaType = mt
			}
		}
		filename, _ := source["filename"].(string)
		doc, err := newKiroDocument(title, mediaType, filename, data)
		return doc, "", nil, err
	case "text":
		text, _ := source["data"].(string)
		if len(text) > maxDocumentBytes {
			return nil, "", nil, newMediaError("document %q is %d bytes; the limit is %d bytes per document", label, len(text), maxDocumentBytes)
		}
		return nil, wrapInlineDocument(title, text), nil, nil
	case "content":
		var parts []string
		var images []KiroImage
		switch c := source["content"].(type) {
		case string:
			parts = append(parts, c)
		default:
			for _, b := range contentBlocksAsMaps(c) {
				switch t, _ := b["type"].(string); t {
				case "text":
					if s, ok := b["text"].(string); ok {
						parts = append(parts, s)
					}
				case "image":
					img := extractImageFromClaudeBlock(b)
					if img == nil {
						return nil, "", nil, newMediaError("document %q contains an image that could not be read", label)
					}
					images = append(images, *img)
				default:
					return nil, "", nil, newMediaError("document %q contains an unsupported %q block", label, t)
				}
			}
		}
		text := strings.Join(parts, "\n")
		if len(text) > maxDocumentBytes {
			return nil, "", nil, newMediaError("document %q is %d bytes; the limit is %d bytes per document", label, len(text), maxDocumentBytes)
		}
		return nil, wrapInlineDocument(title, text), images, nil
	case "url":
		// Downloaded by prepareClaudeMedia before translation.
		return nil, "", nil, newMediaError("document %q has a url source that was not fetched", label)
	case "file":
		return nil, "", nil, newMediaError("document %q uses a file_id source; the Files API is not supported, send the document as base64 data", label)
	default:
		return nil, "", nil, newMediaError("document %q has unsupported source type %q", label, srcType)
	}
}

// claudeDocumentInline is what extractClaudeUserContent folds into the text
// for a document block: the inline text for text sources, and an explicit
// note when a block cannot be attached (validation normally rejects those
// before translation, so the note is a last-resort guard against silent loss).
func claudeDocumentInline(block map[string]interface{}) (string, []KiroImage) {
	doc, text, images, err := claudeDocumentParts(block)
	if err != nil {
		return "[A document could not be attached: " + err.Error() + "]", nil
	}
	if doc != nil {
		return "", nil
	}
	return text, images
}

// extractClaudeDocuments collects the binary documents of a user message,
// including those nested in tool_result content.
func extractClaudeDocuments(content interface{}) []KiroDocument {
	var docs []KiroDocument
	for _, block := range contentBlocksAsMaps(content) {
		switch t, _ := block["type"].(string); t {
		case "document":
			if doc, _, _, err := claudeDocumentParts(block); err == nil && doc != nil {
				docs = append(docs, *doc)
			}
		case "tool_result":
			docs = append(docs, extractClaudeDocuments(block["content"])...)
		}
	}
	return docs
}

// ---- OpenAI file parts ----

// openAIFileFields reads the file object of a Chat `file` part or a
// Responses `input_file` part.
func openAIFileFields(part map[string]interface{}) (filename, fileData, fileID, fileURL string) {
	obj := part
	if f, ok := part["file"].(map[string]interface{}); ok {
		obj = f
	}
	filename, _ = obj["filename"].(string)
	fileData, _ = obj["file_data"].(string)
	fileID, _ = obj["file_id"].(string)
	fileURL, _ = obj["file_url"].(string)
	return
}

func isOpenAIFilePart(part map[string]interface{}) bool {
	t, _ := part["type"].(string)
	return t == "file" || t == "input_file"
}

// openAIFilePart resolves a file part into a document or (for image data) an
// image.
func openAIFilePart(part map[string]interface{}) (*KiroDocument, *KiroImage, error) {
	filename, fileData, fileID, fileURL := openAIFileFields(part)
	label := filename
	if label == "" {
		label = "file"
	}
	if fileData == "" {
		switch {
		case fileID != "":
			return nil, nil, newMediaError("file %q is referenced by file_id; uploaded files are not supported, send file_data as a base64 data URL", label)
		case fileURL != "":
			return nil, nil, newMediaError("file %q has a file_url that was not fetched", label)
		}
		return nil, nil, newMediaError("file %q has no file_data", label)
	}
	mediaType, data, ok := parseAnyDataURL(fileData)
	if !ok {
		// Raw base64 without a data URL prefix: infer from the filename.
		mediaType, data = "", fileData
	}
	if strings.HasPrefix(mediaType, "image/") {
		if img := parseDataURL(fileData); img != nil && isSupportedImageFormat(img.Format) {
			return nil, img, nil
		}
		return nil, nil, newMediaError("file %q has unsupported image type %q", label, mediaType)
	}
	doc, err := newKiroDocument("", mediaType, filename, data)
	return doc, nil, err
}

// extractOpenAIDocuments collects the documents of an OpenAI message.
func extractOpenAIDocuments(content interface{}) []KiroDocument {
	var docs []KiroDocument
	visit := func(part map[string]interface{}) {
		if !isOpenAIFilePart(part) {
			return
		}
		if doc, _, err := openAIFilePart(part); err == nil && doc != nil {
			docs = append(docs, *doc)
		}
	}
	switch c := content.(type) {
	case map[string]interface{}:
		visit(c)
	case []interface{}:
		for _, p := range c {
			if part, ok := p.(map[string]interface{}); ok {
				visit(part)
			}
		}
	}
	return docs
}

// openAIFileImages returns images carried in file parts (file_data with an
// image media type), which the generic image extractor does not read.
func openAIFileImages(content interface{}) []KiroImage {
	var images []KiroImage
	parts, _ := content.([]interface{})
	if m, ok := content.(map[string]interface{}); ok {
		parts = []interface{}{m}
	}
	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok || !isOpenAIFilePart(part) {
			continue
		}
		if extractImageFromOpenAIPart(part) != nil {
			continue // already read by the generic image path
		}
		if _, img, err := openAIFilePart(part); err == nil && img != nil {
			images = append(images, *img)
		}
	}
	return images
}

// openAIFileErrorsText returns notes for file parts that cannot be attached,
// so they are never dropped silently on paths that skipped validation.
func openAIFileErrorsText(content interface{}) string {
	parts, _ := content.([]interface{})
	var notes []string
	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok || !isOpenAIFilePart(part) {
			continue
		}
		if _, _, err := openAIFilePart(part); err != nil {
			notes = append(notes, "[A file could not be attached: "+err.Error()+"]")
		}
	}
	return strings.Join(notes, "\n")
}

// ---- payload finishing ----

var documentNameInvalid = regexp.MustCompile(`[^A-Za-z0-9\s\-\(\)\[\]]+`)
var documentNameSpaces = regexp.MustCompile(`\s+`)

// sanitizeDocumentName keeps the characters the backend accepts in document
// names (letters, digits, single spaces, hyphens, parentheses, brackets).
func sanitizeDocumentName(name, format string) string {
	name = strings.TrimSpace(name)
	if ext := path.Ext(name); ext != "" && strings.EqualFold(strings.TrimPrefix(ext, "."), format) {
		name = strings.TrimSuffix(name, ext)
	}
	name = documentNameInvalid.ReplaceAllString(name, "-")
	name = documentNameSpaces.ReplaceAllString(name, " ")
	name = strings.Trim(name, " -")
	if len(name) > maxDocumentNameLen {
		name = strings.TrimSpace(name[:maxDocumentNameLen])
	}
	if name == "" {
		name = defaultDocumentName
	}
	return name
}

// finalizePayloadDocuments gives every document in the payload a valid name
// that is unique across the request (DOCUMENT_DUPLICATE_NAME).
func finalizePayloadDocuments(payload *KiroPayload) {
	if payload == nil {
		return
	}
	trimHistoryDocuments(payload)
	used := map[string]bool{}
	fix := func(m *KiroUserInputMessage) {
		if m == nil {
			return
		}
		for i := range m.Documents {
			d := &m.Documents[i]
			base := sanitizeDocumentName(d.Name, d.Format)
			name := base
			for n := 2; used[strings.ToLower(name)]; n++ {
				name = fmt.Sprintf("%s (%d)", base, n)
			}
			used[strings.ToLower(name)] = true
			d.Name = name
		}
	}
	for i := range payload.ConversationState.History {
		fix(payload.ConversationState.History[i].UserInputMessage)
	}
	fix(&payload.ConversationState.CurrentMessage.UserInputMessage)
}

// userContentFallback returns the prompt text for a user turn that carries
// only attachments.
func userContentFallback(text string, hasImages, hasDocs bool) string {
	trimmed := strings.TrimSpace(text)
	if trimmed != "" {
		return trimmed
	}
	if hasDocs {
		return documentOnlyUserContent
	}
	return normalizeUserContent(text, hasImages)
}

// mediaBytes is the base64 payload carried by a user message; it does not
// count against the text payload budget (see payloadByteSize).
func mediaBytes(m *KiroUserInputMessage) int {
	if m == nil {
		return 0
	}
	n := 0
	for _, img := range m.Images {
		n += len(img.Source.Bytes)
	}
	for _, d := range m.Documents {
		n += len(d.Source.Bytes)
	}
	return n
}

// documentDataURL renders a document for the /v1/responses input_file part.
func documentDataURL(d KiroDocument) (filename, dataURL string) {
	mt := documentMIMEByFormat[d.Format]
	if mt == "" {
		mt = "application/octet-stream"
	}
	name := d.Name
	if name == "" {
		name = defaultDocumentName
	}
	return name + "." + d.Format, "data:" + mt + ";base64," + d.Source.Bytes
}

// trimHistoryDocuments keeps at most maxDocumentsPerRequest documents in the
// whole payload: the current message's first, then the newest history turns.
// Older documents are removed from history and the turn gets a short note, so
// long conversations with many attachments keep working instead of failing
// on the backend's document count limit.
func trimHistoryDocuments(payload *KiroPayload) {
	remaining := maxDocumentsPerRequest - len(payload.ConversationState.CurrentMessage.UserInputMessage.Documents)
	if remaining < 0 {
		remaining = 0
	}
	hist := payload.ConversationState.History
	for i := len(hist) - 1; i >= 0; i-- {
		m := hist[i].UserInputMessage
		if m == nil || len(m.Documents) == 0 {
			continue
		}
		if len(m.Documents) <= remaining {
			remaining -= len(m.Documents)
			continue
		}
		keep := m.Documents[:remaining]
		dropped := m.Documents[remaining:]
		remaining = 0
		names := make([]string, 0, len(dropped))
		for _, d := range dropped {
			names = append(names, fmt.Sprintf("%q", d.Name))
		}
		note := fmt.Sprintf("[Earlier attachment(s) %s omitted from this request to stay within the per-request document limit.]", strings.Join(names, ", "))
		if strings.TrimSpace(m.Content) == "" || m.Content == documentOnlyUserContent {
			m.Content = note
		} else {
			m.Content += "\n\n" + note
		}
		m.Documents = append([]KiroDocument(nil), keep...)
	}
}
