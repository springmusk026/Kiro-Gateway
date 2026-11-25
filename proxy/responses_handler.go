package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/springmusk026/Kiro-Gateway/config"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultResponsesModel = "claude-sonnet-4.5"

func (h *Handler) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.sendAPIMethodNotAllowed(w, r, "POST")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendBodyReadError(w, r, err)
		return
	}
	body = maybeCompressRequestBody(body)

	var req ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendOpenAIError(w, 400, "invalid_request_error", "Invalid JSON")
		return
	}
	// Keep the client's tool definitions verbatim for the response echo.
	var rawEcho struct {
		Tools json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(body, &rawEcho) == nil {
		req.RawTools = rawEcho.Tools
	}

	if strings.TrimSpace(req.Model) == "" {
		req.Model = defaultResponsesModel
	}
	// Responses (and stored responses) echo the client's model name; routing
	// and request logs use the resolved id.
	r = r.WithContext(withRequestedModel(r.Context(), req.Model))

	storedInputCopy := append(json.RawMessage(nil), req.Input...)

	storeResponse := true
	if req.Store != nil {
		storeResponse = *req.Store
	}

	// Resolved here rather than at the point of use further down: the replay
	// budget below is sized from the model's context window, and a client-supplied
	// name still carrying the thinking suffix matches no known model, so the
	// window lookup would miss and fall back to the smallest tier.
	thinkingCfg := config.GetThinkingConfig()
	actualModel, suffixThinking := ParseModelAndThinking(req.Model, thinkingCfg.Suffix)
	thinkingSpec := resolveThinking(responsesThinkingAsk(&req, suffixThinking), thinkingCfg)
	thinking := thinkingSpec.Enabled
	if msg := disabledModelMessage(actualModel); msg != "" {
		h.sendOpenAIError(w, 400, "invalid_request_error", msg)
		return
	}

	var historyMessages []OpenAIMessage
	if req.PreviousResponseID != "" {
		prev, loadErr := loadResponse(req.PreviousResponseID)
		if loadErr != nil {
			h.sendOpenAIError(w, 404, "invalid_request_error",
				fmt.Sprintf("previous_response_id not found: %v", loadErr))
			return
		}
		historyMessages = expandPreviousResponseHistory(prev, actualModel)
	}

	inputMessages, err := parseResponsesInput(req.Input)
	if err != nil {
		h.sendOpenAIError(w, 400, "invalid_request_error", err.Error())
		return
	}

	finalMessages := make([]OpenAIMessage, 0, len(historyMessages)+len(inputMessages)+1)
	finalMessages = append(finalMessages, historyMessages...)
	if strings.TrimSpace(req.Instructions) != "" {
		// New instructions on this turn always take effect, even when
		// continuing from previous_response_id. Place them after the
		// expanded history so they apply to the current and future turns,
		// while ancestor instructions (re-emitted by expandPreviousResponseHistory)
		// stay in scope for the historical exchanges they shaped.
		finalMessages = append(finalMessages, OpenAIMessage{
			Role:    "system",
			Content: req.Instructions,
		})
	}
	finalMessages = append(finalMessages, inputMessages...)

	if len(finalMessages) == 0 {
		h.sendOpenAIError(w, 400, "invalid_request_error", "input must contain at least one message")
		return
	}

	hasUser := false
	for _, m := range finalMessages {
		if m.Role == "user" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		h.sendOpenAIError(w, 400, "invalid_request_error", "input must contain at least one user message")
		return
	}
	// input_image URLs and input_file items are downloaded / validated here,
	// replayed history included (document limits are per request).
	if err := prepareOpenAIMedia(r.Context(), finalMessages); err != nil {
		status, errType, msg := mediaErrorStatus(err)
		if status == 500 {
			errType = "server_error"
		}
		h.sendOpenAIError(w, status, errType, msg)
		return
	}

	openaiReq := &OpenAIRequest{
		Model:    req.Model,
		Messages: finalMessages,
		Stream:   req.Stream,
		Tools:    req.Tools,
	}
	// Codex Responses Lite (gpt-5.6 and newer) omits the top-level tools array
	// and ships tool definitions inside an additional_tools input item instead.
	// Without this merge the request reaches Kiro carrying no tools at all, and
	// the model correctly reports that it has no shell available.
	if extra := extractResponsesAdditionalTools(req.Input); len(extra) > 0 {
		openaiReq.Tools = append(openaiReq.Tools, extra...)
	}
	// Unwrap Codex namespace wrappers up front so the token estimator and cache
	// profile see the real tool schemas rather than just the wrapper name. Tool
	// schemas are large, so leaving them nested materially skews both.
	openaiReq.Tools = flattenOpenAITools(openaiReq.Tools, 0)
	customTools := collectCustomToolNames(openaiReq.Tools)
	toolNamespaces := collectToolNamespaces(openaiReq.Tools)
	if len(req.ToolChoice) > 0 {
		var toolChoice interface{}
		if err := json.Unmarshal(req.ToolChoice, &toolChoice); err == nil {
			openaiReq.ToolChoice = toolChoice
		}
	}
	if req.Temperature != nil {
		openaiReq.Temperature = *req.Temperature
	}
	if req.MaxOutputTokens != nil {
		openaiReq.MaxTokens = *req.MaxOutputTokens
	}
	if req.TopP != nil {
		openaiReq.TopP = *req.TopP
	}
	openaiReq.ParallelToolCalls = req.ParallelToolCalls

	openaiReq.Model = actualModel

	estimatedInputTokens := estimateOpenAIRequestInputTokens(openaiReq)
	openaiReq.thinkingBudget = thinkingSpec.Budget
	kiroPayload := OpenAIToKiro(openaiReq, thinking)
	applyThinkingSpec(kiroPayload, actualModel, thinkingSpec)
	cacheProfile := h.promptCache.BuildOpenAIProfile(openaiReq, estimatedInputTokens)

	apiKeyID := apiKeyIDFromContext(r.Context())
	respID := generateResponseID()

	if req.Stream {
		h.handleResponsesStream(r.Context(), w, kiroPayload, actualModel, thinking, estimatedInputTokens,
			apiKeyID, respID, &req, storedInputCopy, storeResponse, cacheProfile, customTools, toolNamespaces)
		return
	}

	h.handleResponsesNonStream(r.Context(), w, kiroPayload, actualModel, thinking, estimatedInputTokens,
		apiKeyID, respID, &req, storedInputCopy, storeResponse, cacheProfile, customTools, toolNamespaces)
}

func (h *Handler) handleResponsesNonStream(
	ctx context.Context, w http.ResponseWriter, payload *KiroPayload, model string, thinking bool,
	estimatedInputTokens int, apiKeyID, respID string,
	req *ResponsesRequest, storedInput json.RawMessage, storeResponse bool,
	cacheProfile *promptCacheProfile, customTools map[string]bool, toolNamespaces map[string]string,
) {
	attempt := func(a *upstreamAttempt) upstreamAttemptRun {
		model = a.model
		cacheUsage := a.cacheUsage

		var content, reasoningContent string
		var toolUses []KiroToolUse
		var inputTokens, outputTokens int
		var credits float64
		var realInputTokens int
		var upstreamStopReason string

		callback := &KiroStreamCallback{
			OnText: func(text string, isThinking bool) {
				if isThinking {
					reasoningContent += text
				} else {
					content += text
				}
			},
			OnToolUse:  func(tu KiroToolUse) { toolUses = append(toolUses, tu) },
			OnComplete: func(inTok, outTok int) { inputTokens = inTok; outputTokens = outTok },
			OnCredits:  func(c float64) { credits = c },
			OnContextUsage: func(pct float64) {
				realInputTokens = int(pct * float64(getContextWindowSize(model)) / 100.0)
			},
			OnStopReason: func(reason string) {
				upstreamStopReason = reason
			},
		}

		measure := func() (int, int, string, bool) {
			return len(content), len(toolUses), upstreamStopReason, reasoningContent != ""
		}

		reset := func() {
			content = ""
			reasoningContent = ""
			toolUses = nil
			inputTokens = 0
			outputTokens = 0
			credits = 0
			realInputTokens = 0
			upstreamStopReason = ""
		}

		// Fully buffered path: nothing reaches the client until the response is
		// encoded, so a retry can never duplicate output.
		var finalContent string
		finish := func() upstreamUsage {
			var tagReasoning string
			finalContent, tagReasoning = extractThinkingFromContent(content)
			if reasoningContent == "" {
				reasoningContent = tagReasoning
			}
			if !thinking {
				reasoningContent = ""
			}

			inputTokens = resolveInputTokens(realInputTokens, inputTokens, estimatedInputTokens)
			outputTokens = estimateOpenAIOutputTokens(finalContent, reasoningContent, toolUses)
			return upstreamUsage{inputTokens: inputTokens, outputTokens: outputTokens, credits: credits}
		}

		respond := func(upstreamUsage) {
			respObj := buildResponsesObject(respID, echoModel(ctx, model), finalContent, toolUses, inputTokens, outputTokens, req, upstreamStopReason, cacheUsage, cacheProfile != nil, customTools, toolNamespaces)
			var reasoningItems []ResponseOutputItem
			if strings.TrimSpace(reasoningContent) != "" {
				reasoningItems = append(reasoningItems, newReasoningOutputItem(generateOutputItemID("rs"), reasoningContent))
			}
			applyReasoningToResponse(respObj, reasoningItems, reasoningContent)
			respObj.StoredInput = storedInput
			respObj.Instructions = req.Instructions

			if storeResponse {
				if saveErr := saveResponse(respObj); saveErr != nil {
					logResponsesPersistFailure(respObj.ID, saveErr)
				}
			}

			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(respObj)
		}

		return upstreamAttemptRun{
			callback: callback,
			measure:  measure,
			reset:    reset,
			finish:   finish,
			respond:  respond,
		}
	}

	h.executeUpstream(ctx, w, &upstreamRequest{
		endpoint:       "responses",
		payload:        payload,
		model:          model,
		apiKeyID:       apiKeyID,
		cacheProfile:   cacheProfile,
		attempt:        attempt,
		failNoAccounts: func(model string) { h.sendOpenAINoAccounts(w, model) },
		fail:           func(_ string, err error) { h.sendOpenAIUpstreamError(w, err) },
	})
}

func mapResponsesCompletion(reason string) (status, incompleteReason string) {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "max_tokens", "max_output_tokens", "length", "model_context_window_exceeded", "context_window_exceeded":
		return "incomplete", "max_output_tokens"
	case "refusal", "content_filter", "content_filtered", "guardrail_intervened":
		return "incomplete", "content_filter"
	default:
		return "completed", ""
	}
}

func buildResponsesObject(
	id, model, content string, toolUses []KiroToolUse,
	inputTokens, outputTokens int, req *ResponsesRequest, upstreamStopReason string,
	cacheUsage promptCacheUsage, includeCache bool, customTools map[string]bool, toolNamespaces map[string]string,
) *ResponsesObject {
	output := make([]ResponseOutputItem, 0, 1+len(toolUses))

	if strings.TrimSpace(content) != "" {
		output = append(output, ResponseOutputItem{
			ID:     generateOutputItemID("msg"),
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []ResponseContentPart{{
				Type: "output_text",
				Text: content,
			}},
		})
	}

	for _, tu := range toolUses {
		shape := shapeResponsesToolCall(tu, customTools, toolNamespaces)
		item := ResponseOutputItem{
			ID:        generateOutputItemID("fc"),
			Type:      shape.Type,
			Status:    "completed",
			CallID:    tu.ToolUseID,
			Name:      tu.Name,
			Namespace: shape.Namespace,
		}
		if shape.Type == responsesCustomToolCallType {
			item.Input = shape.Input
		} else {
			item.Arguments = shape.Arguments
		}
		output = append(output, item)
	}

	if len(output) == 0 {
		output = append(output, ResponseOutputItem{
			ID:     generateOutputItemID("msg"),
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []ResponseContentPart{{
				Type: "output_text",
				Text: "",
			}},
		})
	}

	status, incompleteReason := mapResponsesCompletion(upstreamStopReason)
	var incompleteDetails *ResponsesIncompleteDetails
	if incompleteReason != "" {
		incompleteDetails = &ResponsesIncompleteDetails{Reason: incompleteReason}
	}

	var inputTokensDetails *ResponsesInputTokensDetails
	if includeCache {
		// Shared with the Chat Completions path: OpenAI reports input_tokens as
		// the raw total with the cached portion as a subset of it, not excluded
		// from it. See reconcileOpenAICacheUsage.
		cachedTokens := 0
		inputTokens, cachedTokens = reconcileOpenAICacheUsage(inputTokens, cacheUsage)
		inputTokensDetails = &ResponsesInputTokensDetails{CachedTokens: cachedTokens}
	}

	var echoReq ResponsesRequest
	if req != nil {
		echoReq = *req
	}
	req = &echoReq
	obj := &ResponsesObject{
		ID:        id,
		Object:    "response",
		CreatedAt: time.Now().Unix(),
		Status:    status,
		Model:     model,
		Output:    output,
		Usage: ResponsesUsage{
			InputTokens:        inputTokens,
			InputTokensDetails: inputTokensDetails,
			OutputTokens:       outputTokens,
			TotalTokens:        inputTokens + outputTokens,
		},
		PreviousResponseID: req.PreviousResponseID,
		Metadata:           req.Metadata,
		IncompleteDetails:  incompleteDetails,
	}
	obj.applyRequestEcho(req, req.RawTools)
	return obj
}

func (h *Handler) handleResponsesStream(
	ctx context.Context, w http.ResponseWriter, payload *KiroPayload, model string, thinking bool,
	estimatedInputTokens int, apiKeyID, respID string,
	req *ResponsesRequest, storedInput json.RawMessage, storeResponse bool,
	cacheProfile *promptCacheProfile, customTools map[string]bool, toolNamespaces map[string]string,
) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.sendOpenAIError(w, 500, "server_error", "Streaming not supported")
		return
	}
	hb := startSSEHeartbeat(ctx, w, flusher, sseKeepAliveFrame)
	defer hb.Stop()
	w, flusher = hb, hb

	// Every event carries a sequence_number, as the official stream does.
	seq := 0
	send := func(eventName string, payload map[string]interface{}) {
		payload["sequence_number"] = seq
		seq++
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, string(data))
		flusher.Flush()
	}

	createdAt := time.Now().Unix()
	initial := &ResponsesObject{
		ID:                 respID,
		Object:             "response",
		CreatedAt:          createdAt,
		Status:             "in_progress",
		Model:              echoModel(ctx, model),
		Output:             []ResponseOutputItem{},
		Usage:              ResponsesUsage{},
		PreviousResponseID: req.PreviousResponseID,
		Metadata:           req.Metadata,
		Instructions:       req.Instructions,
	}
	initial.applyRequestEcho(req, req.RawTools)
	send("response.created", map[string]interface{}{
		"type":     "response.created",
		"response": initial,
	})
	// Sent once per response, not once per account attempt.
	send("response.in_progress", map[string]interface{}{
		"type":     "response.in_progress",
		"response": initial,
	})

	// sendFailed ends the stream with a response.failed event carrying a full
	// response object, the items already completed and a classified error.
	sendFailed := func(output []ResponseOutputItem, code, message string) {
		failed := *initial
		failed.Status = "failed"
		failed.Output = output
		if failed.Output == nil {
			failed.Output = []ResponseOutputItem{}
		}
		failed.Error = &ResponsesError{Code: code, Message: message}
		send("response.failed", map[string]interface{}{
			"type":     "response.failed",
			"response": &failed,
		})
	}

	responseStarted := false
	// attemptDoneItems reads the current attempt's completed output items.
	var attemptDoneItems func() []ResponseOutputItem

	attempt := func(a *upstreamAttempt) upstreamAttemptRun {
		model = a.model
		cacheUsage := a.cacheUsage

		var (
			fullText           strings.Builder
			msgText            strings.Builder
			reasoningText      strings.Builder
			toolUses           []KiroToolUse
			doneItems          []ResponseOutputItem
			inputTokens        int
			outputTokens       int
			credits            float64
			realInputTokens    int
			upstreamStopReason string
		)

		messageItemID := generateOutputItemID("msg")
		messageStarted := false
		messagesClosed := 0
		outputIndex := 0
		contentIndex := 0

		// Reasoning (when thinking is on) streams as its own output item ahead
		// of the message it precedes.
		reasoningStream := &responsesReasoningStream{send: send}
		var reasoningItems []ResponseOutputItem
		closeReasoning := func() {
			if item, ok := reasoningStream.close(); ok {
				doneItems = append(doneItems, item)
				reasoningItems = append(reasoningItems, item)
				outputIndex++
			}
		}

		ensureMessageStarted := func() {
			if messageStarted {
				return
			}
			messageStarted = true
			send("response.output_item.added", map[string]interface{}{
				"type":         "response.output_item.added",
				"output_index": outputIndex,
				"item": map[string]interface{}{
					"id":      messageItemID,
					"type":    "message",
					"role":    "assistant",
					"status":  "in_progress",
					"content": []interface{}{},
				},
			})
			send("response.content_part.added", map[string]interface{}{
				"type":          "response.content_part.added",
				"item_id":       messageItemID,
				"output_index":  outputIndex,
				"content_index": contentIndex,
				"part":          ResponseContentPart{Type: "output_text"},
			})
		}

		// closeMessage finishes the open message item with the official
		// done-event sequence: output_text.done, content_part.done,
		// output_item.done.
		closeMessage := func(text string) {
			if !messageStarted {
				return
			}
			part := ResponseContentPart{Type: "output_text", Text: text}
			send("response.output_text.done", map[string]interface{}{
				"type":          "response.output_text.done",
				"item_id":       messageItemID,
				"output_index":  outputIndex,
				"content_index": contentIndex,
				"text":          text,
				"logprobs":      []interface{}{},
			})
			send("response.content_part.done", map[string]interface{}{
				"type":          "response.content_part.done",
				"item_id":       messageItemID,
				"output_index":  outputIndex,
				"content_index": contentIndex,
				"part":          part,
			})
			item := ResponseOutputItem{
				ID:      messageItemID,
				Type:    "message",
				Role:    "assistant",
				Status:  "completed",
				Content: []ResponseContentPart{part},
			}
			send("response.output_item.done", map[string]interface{}{
				"type":         "response.output_item.done",
				"output_index": outputIndex,
				"item":         item,
			})
			doneItems = append(doneItems, item)
			messageStarted = false
			messagesClosed++
			msgText.Reset()
			messageItemID = generateOutputItemID("msg")
			outputIndex++
		}

		callback := &KiroStreamCallback{
			OnText: func(text string, isThinking bool) {
				if text == "" {
					return
				}
				if isThinking {
					reasoningText.WriteString(text)
					if thinking {
						closeMessage(msgText.String())
						reasoningStream.delta(outputIndex, text)
						responseStarted = true
					}
					return
				}
				closeReasoning()
				fullText.WriteString(text)
				msgText.WriteString(text)
				ensureMessageStarted()
				send("response.output_text.delta", map[string]interface{}{
					"type":          "response.output_text.delta",
					"item_id":       messageItemID,
					"output_index":  outputIndex,
					"content_index": contentIndex,
					"delta":         text,
					"logprobs":      []interface{}{},
				})
				responseStarted = true
			},
			OnToolUse: func(tu KiroToolUse) {
				closeReasoning()
				closeMessage(msgText.String())

				toolUses = append(toolUses, tu)
				shape := shapeResponsesToolCall(tu, customTools, toolNamespaces)
				fcID := generateOutputItemID("fc")
				send("response.output_item.added", map[string]interface{}{
					"type":         "response.output_item.added",
					"output_index": outputIndex,
					"item":         shape.outputItem(fcID, tu, "in_progress", false),
				})
				send(shape.deltaEvent(), shape.deltaPayload(fcID, outputIndex))
				send(shape.doneEvent(), shape.donePayload(fcID, outputIndex, tu.Name))
				// Codex reads tool calls only from output_item.done, ignoring the
				// delta events above, so this event must carry the complete call.
				send("response.output_item.done", map[string]interface{}{
					"type":         "response.output_item.done",
					"output_index": outputIndex,
					"item":         shape.outputItem(fcID, tu, "completed", true),
				})
				doneItem := ResponseOutputItem{
					ID:        fcID,
					Type:      shape.Type,
					Status:    "completed",
					CallID:    tu.ToolUseID,
					Name:      tu.Name,
					Namespace: shape.Namespace,
				}
				if shape.Type == responsesCustomToolCallType {
					doneItem.Input = shape.Input
				} else {
					doneItem.Arguments = shape.Arguments
				}
				doneItems = append(doneItems, doneItem)
				outputIndex++
				responseStarted = true
			},
			OnComplete: func(inTok, outTok int) { inputTokens = inTok; outputTokens = outTok },
			OnCredits:  func(c float64) { credits = c },
			OnContextUsage: func(pct float64) {
				realInputTokens = int(pct * float64(getContextWindowSize(model)) / 100.0)
			},
			OnStopReason: func(reason string) {
				upstreamStopReason = reason
			},
		}

		measure := func() (int, int, string, bool) {
			return fullText.Len(), len(toolUses), upstreamStopReason, reasoningText.Len() > 0
		}

		// Retries only run while responseStarted is false, i.e. before any
		// content or function-call item has been sent, so the output_index /
		// content_index cursors are still untouched. Only the accumulators need
		// clearing.
		reset := func() {
			fullText.Reset()
			msgText.Reset()
			reasoningText.Reset()
			toolUses = nil
			doneItems = nil
			reasoningItems = nil
			inputTokens = 0
			outputTokens = 0
			credits = 0
			realInputTokens = 0
			upstreamStopReason = ""
		}

		var finalContent, reasoning string
		finish := func() upstreamUsage {
			finalContent, _ = extractThinkingFromContent(fullText.String())
			reasoning = reasoningText.String()
			if !thinking {
				reasoning = ""
			}

			closeReasoning()
			if messageStarted {
				text := msgText.String()
				if messagesClosed == 0 {
					// A single message: report the same text as response.completed.
					text = finalContent
				}
				closeMessage(text)
			}

			inputTokens = resolveInputTokens(realInputTokens, inputTokens, estimatedInputTokens)
			outputTokens = estimateOpenAIOutputTokens(finalContent, reasoning, toolUses)
			return upstreamUsage{inputTokens: inputTokens, outputTokens: outputTokens, credits: credits}
		}

		respond := func(upstreamUsage) {
			respObj := buildResponsesObject(respID, echoModel(ctx, model), finalContent, toolUses, inputTokens, outputTokens, req, upstreamStopReason, cacheUsage, cacheProfile != nil, customTools, toolNamespaces)
			applyReasoningToResponse(respObj, reasoningItems, reasoning)
			respObj.CreatedAt = createdAt
			respObj.StoredInput = storedInput
			respObj.Instructions = req.Instructions

			if storeResponse {
				if saveErr := saveResponse(respObj); saveErr != nil {
					logResponsesPersistFailure(respObj.ID, saveErr)
				}
			}

			// The official stream ends with response.completed; there is no
			// trailing "data: [DONE]".
			send("response.completed", map[string]interface{}{
				"type":     "response.completed",
				"response": respObj,
			})
		}

		attemptDoneItems = func() []ResponseOutputItem { return doneItems }
		return upstreamAttemptRun{
			callback: callback,
			measure:  measure,
			reset:    reset,
			finish:   finish,
			respond:  respond,
		}
	}

	h.executeUpstream(ctx, w, &upstreamRequest{
		endpoint:     "responses",
		payload:      payload,
		model:        model,
		apiKeyID:     apiKeyID,
		cacheProfile: cacheProfile,
		started:      func() bool { return responseStarted },
		// An error after output started ends the response without counting
		// against the account.
		keepAccountAfterOutput: true,
		attempt:                attempt,
		failStarted: func(_ *upstreamAttempt, err error) {
			info := classifyUpstreamError(err)
			sendFailed(attemptDoneItems(), info.Code, info.Message)
		},
		failNoAccounts: func(string) { sendFailed(nil, "server_overloaded", noAvailableAccountsMessage) },
		fail: func(_ string, err error) {
			info := classifyUpstreamError(err)
			sendFailed(nil, info.Code, info.Message)
		},
	})
}

// handleResponseByID serves GET and DELETE /v1/responses/{id} from the
// response store. Responses here never run in background mode, so cancel is
// always rejected as the official API rejects it for non-background ones.
func (h *Handler) handleResponseByID(w http.ResponseWriter, r *http.Request, rest string) {
	rest = strings.Trim(rest, "/")
	id, action, _ := strings.Cut(rest, "/")
	if id == "" {
		h.sendAPINotFound(w, r)
		return
	}
	notFound := func() {
		h.sendOpenAIErrorWithCode(w, http.StatusNotFound, "invalid_request_error", "",
			fmt.Sprintf("No response found with id '%s'.", id))
	}

	switch action {
	case "cancel":
		if r.Method != http.MethodPost {
			h.sendAPIMethodNotAllowed(w, r, "POST")
			return
		}
		if _, err := loadResponse(id); err != nil {
			notFound()
			return
		}
		h.sendOpenAIErrorWithCode(w, http.StatusBadRequest, "invalid_request_error", "",
			"Only responses created with background=true can be cancelled.")
		return
	case "":
	default:
		h.sendAPINotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		resp, err := loadResponse(id)
		if err != nil {
			notFound()
			return
		}
		resp.fillWireDefaults()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(resp)
	case http.MethodDelete:
		deleted, err := deleteResponse(id)
		if err != nil {
			h.sendOpenAIError(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
		if !deleted {
			notFound()
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      id,
			"object":  "response",
			"deleted": true,
		})
	default:
		h.sendAPIMethodNotAllowed(w, r, "GET, DELETE")
	}
}
