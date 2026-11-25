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
	"unicode/utf8"

	"github.com/google/uuid"
)

// handleOpenAIChat OpenAI API 处理
func (h *Handler) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
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

	var req OpenAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendOpenAIError(w, 400, "invalid_request_error", "Invalid JSON")
		return
	}
	if msg := validateOpenAIRequestShape(&req); msg != "" {
		h.sendOpenAIError(w, 400, "invalid_request_error", msg)
		return
	}
	// Responses echo the client's model name; routing uses the resolved id.
	r = r.WithContext(withRequestedModel(r.Context(), req.Model))

	// 解析模型和 thinking 模式
	thinkingCfg := config.GetThinkingConfig()
	actualModel, suffixThinking := ParseModelAndThinking(req.Model, thinkingCfg.Suffix)
	thinkingSpec := resolveThinking(openAIThinkingAsk(&req, suffixThinking), thinkingCfg)
	thinking := thinkingSpec.Enabled
	req.thinkingBudget = thinkingSpec.Budget
	req.Model = actualModel
	if msg := disabledModelMessage(actualModel); msg != "" {
		h.sendOpenAIError(w, 400, "invalid_request_error", msg)
		return
	}
	if err := prepareOpenAIMedia(r.Context(), req.Messages); err != nil {
		status, errType, msg := mediaErrorStatus(err)
		if status == 500 {
			errType = "server_error"
		}
		h.sendOpenAIError(w, status, errType, msg)
		return
	}
	estimatedInputTokens := estimateOpenAIRequestInputTokens(&req)
	cacheProfile := h.promptCache.BuildOpenAIProfile(&req, estimatedInputTokens)

	kiroPayload := OpenAIToKiro(&req, thinking)
	applyThinkingSpec(kiroPayload, actualModel, thinkingSpec)

	apiKeyID := apiKeyIDFromContext(r.Context())
	ctx := withStopSequences(r.Context(), openAIStopSequences(req.Stop))
	if req.StreamOptions != nil && req.StreamOptions.IncludeUsage {
		ctx = context.WithValue(ctx, includeUsageKey{}, true)
	}
	if req.Stream {
		h.handleOpenAIStream(ctx, w, kiroPayload, req.Model, thinking, estimatedInputTokens, cacheProfile, apiKeyID)
	} else {
		h.handleOpenAINonStream(ctx, w, kiroPayload, req.Model, thinking, estimatedInputTokens, cacheProfile, apiKeyID)
	}
}

// handleOpenAIStream OpenAI 流式响应
func (h *Handler) handleOpenAIStream(ctx context.Context, w http.ResponseWriter, payload *KiroPayload, model string, thinking bool, estimatedInputTokens int, cacheProfile *promptCacheProfile, apiKeyID string) {
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

	// 获取 thinking 输出格式配置
	thinkingFormat := config.GetThinkingConfig().OpenAIFormat

	chatID := "chatcmpl-" + uuid.New().String()
	created := time.Now().Unix()
	roleSent := false
	// writeChunk emits one chunk. The first chunk of a stream announces the
	// assistant role, as the official API does.
	writeRole := func() {
		if !roleSent {
			roleSent = true
			roleData, _ := json.Marshal(map[string]interface{}{
				"id":      chatID,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   echoModel(ctx, model),
				"choices": []map[string]interface{}{{
					"index":         0,
					"delta":         map[string]interface{}{"role": "assistant", "content": ""},
					"logprobs":      nil,
					"finish_reason": nil,
				}},
			})
			writeSSEData(w, roleData)
		}
	}
	writeChunk := func(chunk map[string]interface{}) {
		writeRole()
		data, _ := json.Marshal(chunk)
		writeSSEData(w, data)
		flusher.Flush()
	}
	// writeDelta emits a content (or reasoning_content) chunk, hand-encoded
	// with the same bytes as the map form writeChunk would marshal.
	writeDelta := func(field, text string) {
		writeRole()
		writeOpenAIDeltaChunk(w, chatID, created, echoModel(ctx, model), field, text)
		flusher.Flush()
	}
	responseStarted := false

	attempt := func(a *upstreamAttempt) upstreamAttemptRun {
		model = a.model
		cacheUsage := a.cacheUsage

		var upstreamStopReason string
		var toolCalls []ToolCall
		var toolCallIndex int
		var inputTokens, outputTokens int
		var credits float64
		var realInputTokens int
		var rawContentBuilder strings.Builder
		var rawReasoningBuilder strings.Builder
		var textBuffer string
		var inThinkingBlock bool
		var dropTagThinking bool
		var thinkingSource thinkingStreamSource
		var thinkingStarted bool
		var eventThinkingOpen bool

		// Stop sequences: see handleClaudeStream.
		stopper := newStopSequenceMatcher(stopSequencesFrom(ctx))
		cancelUpstream := a.cancel
		stopBypass := false

		sendChunk := func(content string, thinkingState int) {
			if content == "" && thinkingState == 2 {
				return
			}

			// The chunk is a single delta field (see writeDelta).
			var deltaField, deltaText string

			if thinkingState > 0 {
				if !thinking {
					return
				}
				switch thinkingFormat {
				case "thinking":
					var text string
					switch thinkingState {
					case 1:
						text = "<thinking>" + content
					case 2:
						text = content
					case 3:
						text = content + "</thinking>"
					}
					if text == "" {
						return
					}
					deltaField, deltaText = "content", text
				case "think":
					var text string
					switch thinkingState {
					case 1:
						text = "<think>" + content
					case 2:
						text = content
					case 3:
						text = content + "</think>"
					}
					if text == "" {
						return
					}
					deltaField, deltaText = "content", text
				default:
					if content == "" {
						return
					}
					deltaField, deltaText = "reasoning_content", content
				}
			} else {
				if stopper != nil && !stopBypass {
					content = stopper.Push(content)
					if stopper.Done() {
						cancelUpstream()
					}
				}
				if content == "" {
					return
				}
				deltaField, deltaText = "content", content
			}
			writeDelta(deltaField, deltaText)
			responseStarted = true
		}

		processText := func(text string, isThinking bool, forceFlush bool) {
			if isThinking && !thinking {
				return
			}

			if isThinking {
				if !allowReasoningSource(&thinkingSource) {
					return
				}
				if !thinkingStarted {
					sendChunk(text, 1)
					thinkingStarted = true
					eventThinkingOpen = true
				} else {
					sendChunk(text, 2)
				}
				return
			}

			if eventThinkingOpen {
				sendChunk("", 3)
				eventThinkingOpen = false
				thinkingStarted = false
			}

			textBuffer += text

			for {
				if !inThinkingBlock {
					thinkingStart := strings.Index(textBuffer, "<thinking>")
					if thinkingStart != -1 {
						if thinkingStart > 0 {
							sendChunk(textBuffer[:thinkingStart], 0)
						}
						textBuffer = textBuffer[thinkingStart+10:]
						inThinkingBlock = true
						dropTagThinking = !allowTagSource(&thinkingSource)
						thinkingStarted = false
					} else if runeCount := utf8.RuneCountInString(textBuffer); forceFlush || runeCount > 50 {
						safeLen := runeCount
						if !forceFlush {
							safeLen = max(0, runeCount-15)
						}
						if safeLen > 0 {
							head, tail := splitRunes(textBuffer, safeLen)
							sendChunk(head, 0)
							textBuffer = tail
						}
						break
					} else {
						break
					}
				} else {
					thinkingEnd := strings.Index(textBuffer, "</thinking>")
					if thinkingEnd != -1 {
						content := textBuffer[:thinkingEnd]
						if !dropTagThinking {
							if !thinkingStarted {
								sendChunk(content, 1)
								sendChunk("", 3)
							} else {
								sendChunk(content, 3)
							}
						}
						textBuffer = textBuffer[thinkingEnd+11:]
						inThinkingBlock = false
						dropTagThinking = false
						thinkingStarted = false
					} else if forceFlush {
						if textBuffer != "" {
							if !dropTagThinking {
								if !thinkingStarted {
									sendChunk(textBuffer, 1)
									sendChunk("", 3)
								} else {
									sendChunk(textBuffer, 3)
								}
							}
							textBuffer = ""
						}
						inThinkingBlock = false
						dropTagThinking = false
						thinkingStarted = false
						break
					} else {
						if runeCount := utf8.RuneCountInString(textBuffer); runeCount > 20 {
							safeLen := runeCount - 15
							if safeLen > 0 {
								head, tail := splitRunes(textBuffer, safeLen)
								if !dropTagThinking {
									if !thinkingStarted {
										sendChunk(head, 1)
										thinkingStarted = true
									} else {
										sendChunk(head, 2)
									}
								}
								textBuffer = tail
							}
						}
						break
					}
				}
			}
		}

		flushStopText := func() {
			if rest := stopper.Flush(); rest != "" {
				stopBypass = true
				sendChunk(rest, 0)
				stopBypass = false
			}
		}

		callback := &KiroStreamCallback{
			OnText: func(text string, isThinking bool) {
				if text == "" || stopper.Done() {
					return
				}
				if isThinking {
					rawReasoningBuilder.WriteString(text)
				} else {
					rawContentBuilder.WriteString(text)
				}
				processText(text, isThinking, false)
			},
			OnToolUse: func(tu KiroToolUse) {
				if stopper.Done() {
					return
				}
				processText("", false, true)
				flushStopText()
				if stopper.Done() {
					return
				}

				args, _ := json.Marshal(tu.Input)
				rawContentBuilder.WriteString(tu.Name)
				rawContentBuilder.Write(args)
				tc := ToolCall{ID: tu.ToolUseID, Type: "function"}
				tc.Function.Name = tu.Name
				tc.Function.Arguments = string(args)
				toolCalls = append(toolCalls, tc)

				chunk := map[string]interface{}{
					"id":      chatID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   echoModel(ctx, model),
					"choices": []map[string]interface{}{{
						"index": 0,
						"delta": map[string]interface{}{
							"tool_calls": []map[string]interface{}{{
								"index": toolCallIndex,
								"id":    tu.ToolUseID,
								"type":  "function",
								"function": map[string]string{
									"name":      tu.Name,
									"arguments": string(args),
								},
							}},
						},
						"logprobs":      nil,
						"finish_reason": nil,
					}},
				}
				toolCallIndex++
				writeChunk(chunk)
				responseStarted = true
			},
			OnStopReason: func(reason string) {
				upstreamStopReason = reason
			},
			OnComplete: func(inTok, outTok int) {
				inputTokens = inTok
				outputTokens = outTok
			},
			OnCredits: func(c float64) {
				credits = c
			},
			OnContextUsage: func(pct float64) {
				realInputTokens = int(pct * float64(getContextWindowSize(model)) / 100.0)
			},
		}

		measure := func() (int, int, string, bool) {
			return rawContentBuilder.Len(), len(toolCalls), upstreamStopReason, rawReasoningBuilder.Len() > 0
		}

		// Retries only happen before anything is flushed, so chunk indices stay
		// valid. The thinking tag parser state must be cleared too: processText
		// holds back up to 50 runes, so a short truncated attempt would
		// otherwise prepend its leftovers to the retry's first chunk.
		reset := func() {
			rawContentBuilder.Reset()
			rawReasoningBuilder.Reset()
			toolCalls = nil
			toolCallIndex = 0
			inputTokens = 0
			outputTokens = 0
			credits = 0
			realInputTokens = 0
			upstreamStopReason = ""
			textBuffer = ""
			inThinkingBlock = false
			dropTagThinking = false
			thinkingSource = thinkingSourceUnknown
			thinkingStarted = false
			eventThinkingOpen = false
			stopper = newStopSequenceMatcher(stopSequencesFrom(ctx))
		}

		var reasoningOutput string
		finish := func() upstreamUsage {
			processText("", false, true)
			flushStopText()
			if eventThinkingOpen {
				sendChunk("", 3)
			}

			inputTokens = resolveInputTokens(realInputTokens, inputTokens, estimatedInputTokens)
			outputContent, extractedReasoning := extractThinkingFromContent(rawContentBuilder.String())
			reasoningOutput = rawReasoningBuilder.String()
			if thinking && reasoningOutput == "" && extractedReasoning != "" {
				reasoningOutput = extractedReasoning
			}
			if !thinking {
				reasoningOutput = ""
			}
			outputTokens = estimateApproxTokens(outputContent) + estimateApproxTokens(reasoningOutput)
			for _, tc := range toolCalls {
				outputTokens += estimateApproxTokens(tc.Function.Name)
				outputTokens += estimateApproxTokens(tc.Function.Arguments)
			}
			return upstreamUsage{inputTokens: inputTokens, outputTokens: outputTokens, credits: credits}
		}

		respond := func(upstreamUsage) {
			finishReason := mapOpenAIFinishReason(upstreamStopReason, len(toolCalls))
			if stopper.Done() {
				finishReason = "stop"
			}

			usage := withReasoningTokenDetails(buildOpenAIUsageMap(inputTokens, outputTokens, cacheUsage, cacheProfile != nil), reasoningOutput)
			chunk := map[string]interface{}{
				"id":      chatID,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   echoModel(ctx, model),
				"choices": []map[string]interface{}{{
					"index":         0,
					"delta":         map[string]interface{}{},
					"logprobs":      nil,
					"finish_reason": finishReason,
				}},
			}
			includeUsage, _ := ctx.Value(includeUsageKey{}).(bool)
			if !includeUsage {
				// Not official without stream_options.include_usage, but clients
				// that never ask for it still read usage from the finish chunk.
				chunk["usage"] = usage
			}
			writeChunk(chunk)
			if includeUsage {
				// Official shape: usage arrives in its own final chunk with an
				// empty choices array.
				writeChunk(map[string]interface{}{
					"id":      chatID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   echoModel(ctx, model),
					"choices": []interface{}{},
					"usage":   usage,
				})
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		}

		return upstreamAttemptRun{
			callback: callback,
			measure:  measure,
			reset:    reset,
			stopped:  func() bool { return stopper.Done() },
			finish:   finish,
			respond:  respond,
		}
	}

	h.executeUpstream(ctx, w, &upstreamRequest{
		endpoint:     "openai",
		payload:      payload,
		model:        model,
		apiKeyID:     apiKeyID,
		cacheProfile: cacheProfile,
		started:      func() bool { return responseStarted },
		attempt:      attempt,
		failStarted: func(_ *upstreamAttempt, err error) {
			data, _ := json.Marshal(map[string]interface{}{
				"error": openAIStreamErrorObject(err),
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		failNoAccounts: func() {
			if hb.Committed() {
				data, _ := json.Marshal(map[string]interface{}{
					"error": openAIErrorObject("server_error", "server_overloaded", noAvailableAccountsMessage),
				})
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				return
			}
			h.sendOpenAINoAccounts(w)
		},
		fail: func(_ string, err error) {
			if hb.Committed() {
				// A heartbeat already sent the 200 status line.
				data, _ := json.Marshal(map[string]interface{}{"error": openAIStreamErrorObject(err)})
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				return
			}
			h.sendOpenAIUpstreamError(w, err)
		},
	})
}

// includeUsageKey marks a chat stream whose client set
// stream_options.include_usage.
type includeUsageKey struct{}

// handleOpenAINonStream OpenAI 非流式响应
func (h *Handler) handleOpenAINonStream(ctx context.Context, w http.ResponseWriter, payload *KiroPayload, model string, thinking bool, estimatedInputTokens int, cacheProfile *promptCacheProfile, apiKeyID string) {
	attempt := func(a *upstreamAttempt) upstreamAttemptRun {
		model = a.model
		cacheUsage := a.cacheUsage

		var content string
		var reasoningContent string
		var toolUses []KiroToolUse
		var inputTokens, outputTokens int
		var credits float64
		var realInputTokens int
		var upstreamStopReason string

		stopWatch := newBufferedStopWatch(stopSequencesFrom(ctx), !thinking, a.cancel)

		callback := &KiroStreamCallback{
			OnText: func(text string, isThinking bool) {
				if stopWatch.Hit() {
					return
				}
				if isThinking {
					reasoningContent += text
				} else {
					content += text
					stopWatch.Observe(content, len(text))
				}
			},
			OnToolUse: func(tu KiroToolUse) {
				if !stopWatch.Hit() {
					toolUses = append(toolUses, tu)
				}
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
			stopWatch.Reset()
		}

		// Fully buffered: nothing reaches the client until the response is
		// encoded, so a retry can never duplicate output.
		var finalContent string
		finish := func() upstreamUsage {
			var extractedReasoning, matchedStop string
			finalContent, extractedReasoning = extractThinkingFromContent(content)
			finalContent, toolUses, matchedStop = stopWatch.Apply(finalContent, toolUses)
			if matchedStop != "" {
				upstreamStopReason = "end_turn"
			}
			if thinking && reasoningContent == "" && extractedReasoning != "" {
				reasoningContent = extractedReasoning
			} else if !thinking {
				reasoningContent = ""
			}

			inputTokens = resolveInputTokens(realInputTokens, inputTokens, estimatedInputTokens)
			outputTokens = estimateOpenAIOutputTokens(finalContent, reasoningContent, toolUses)
			return upstreamUsage{inputTokens: inputTokens, outputTokens: outputTokens, credits: credits}
		}

		respond := func(upstreamUsage) {
			thinkingFormat := config.GetThinkingConfig().OpenAIFormat
			resp := KiroToOpenAIResponseWithReasoning(finalContent, reasoningContent, toolUses, inputTokens, outputTokens, echoModel(ctx, model), thinkingFormat, upstreamStopReason, cacheUsage, cacheProfile != nil)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(resp)
		}

		return upstreamAttemptRun{
			callback: callback,
			measure:  measure,
			reset:    reset,
			stopped:  stopWatch.Hit,
			finish:   finish,
			respond:  respond,
		}
	}

	h.executeUpstream(ctx, w, &upstreamRequest{
		endpoint:       "openai",
		payload:        payload,
		model:          model,
		apiKeyID:       apiKeyID,
		cacheProfile:   cacheProfile,
		attempt:        attempt,
		failNoAccounts: func() { h.sendOpenAINoAccounts(w) },
		fail:           func(_ string, err error) { h.sendOpenAIUpstreamError(w, err) },
	})
}

func (h *Handler) sendOpenAIError(w http.ResponseWriter, status int, errType, message string) {
	h.sendOpenAIErrorWithCode(w, status, errType, "", message)
}
