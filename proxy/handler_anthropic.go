package proxy

import (
	"context"
	"encoding/json"
	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// handleCountTokens Token 计数（Claude Code 会调用）
func (h *Handler) handleCountTokens(w http.ResponseWriter, r *http.Request) {
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

	var req ClaudeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendClaudeError(w, 400, "invalid_request_error", "Invalid JSON")
		return
	}
	if msg := validateClaudeThinkingConfig(req.Thinking, req.MaxTokens); msg != "" {
		h.sendClaudeError(w, 400, "invalid_request_error", msg)
		return
	}

	thinkingCfg := config.GetThinkingConfig()
	actualModel, thinkingSpec := resolveClaudeThinking(&req, thinkingCfg)
	thinking := thinkingSpec.Enabled
	req.Model = actualModel
	effectiveReq := cloneClaudeRequestForThinking(&req, thinking)

	estimatedTokens := estimateClaudeRequestInputTokens(effectiveReq)
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]int{"input_tokens": estimatedTokens})
}

// handleClaudeMessages Claude API 处理
func (h *Handler) handleClaudeMessages(w http.ResponseWriter, r *http.Request) {
	h.handleClaudeMessagesInternal(w, r)
}

func (h *Handler) handleClaudeMessagesInternal(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.sendAPIMethodNotAllowed(w, r, "POST")
		return
	}

	// 读取请求
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendBodyReadError(w, r, err)
		return
	}
	body = maybeCompressRequestBody(body)

	var req ClaudeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendClaudeError(w, 400, "invalid_request_error", "Invalid JSON: "+err.Error())
		return
	}
	if msg := validateClaudeRequestShape(&req); msg != "" {
		h.sendClaudeError(w, 400, "invalid_request_error", msg)
		return
	}
	// Responses echo the client's model name; routing uses the resolved id.
	r = r.WithContext(withRequestedModel(r.Context(), req.Model))

	// 解析模型和 thinking 模式
	thinkingCfg := config.GetThinkingConfig()
	actualModel, thinkingSpec := resolveClaudeThinking(&req, thinkingCfg)
	thinking := thinkingSpec.Enabled
	req.Model = actualModel
	if msg := disabledModelMessage(actualModel); msg != "" {
		h.sendClaudeError(w, 400, "invalid_request_error", msg)
		return
	}
	// Download URL images/documents and validate documents before anything
	// is sent upstream.
	if err := prepareClaudeMedia(r.Context(), &req); err != nil {
		status, errType, msg := mediaErrorStatus(err)
		h.sendClaudeError(w, status, errType, msg)
		return
	}
	effectiveReq := cloneClaudeRequestForThinking(&req, thinking)
	thinkingResponseOpts := resolveClaudeThinkingResponseOptions(req.Thinking, thinkingCfg.ClaudeFormat)
	estimatedInputTokens := estimateClaudeRequestInputTokens(effectiveReq)
	cacheProfile := h.promptCache.BuildClaudeProfile(effectiveReq, estimatedInputTokens)

	apiKeyID := apiKeyIDFromContext(r.Context())

	// Claude Code checks the endpoint at start-up with a one-token "quota"
	// request. Answering it locally saves an upstream request (and its
	// credits) on every client launch.
	if !req.Stream && isClaudeQuotaProbe(&req) {
		writeClaudeProbeResponse(w, echoModel(r.Context(), req.Model))
		return
	}

	// Pure native web_search: relay via Kiro MCP (generateAssistantResponse does not run it).
	if hasWebSearchTool(&req) {
		h.handleWebSearchRequest(r.Context(), w, &req, estimatedInputTokens, apiKeyID)
		return
	}

	// Mixed tools including native web_search: agentic loop digests web_search internally
	// and returns client tool_use blocks as-is.
	if hasWebSearchAmongTools(&req) {
		logger.Infof("[WebSearch] Mixed tools with native web_search, entering agentic loop")
		h.runWebSearchLoop(r.Context(), w, &req, thinking, estimatedInputTokens, apiKeyID)
		return
	}

	// 转换请求
	kiroPayload := ClaudeToKiro(&req, thinking)
	applyThinkingSpec(kiroPayload, actualModel, thinkingSpec)

	// Stream or non-stream
	ctx := withStopSequences(r.Context(), req.StopSequences)
	if req.Stream {
		h.handleClaudeStream(ctx, w, kiroPayload, req.Model, thinking, thinkingResponseOpts, estimatedInputTokens, cacheProfile, apiKeyID)
	} else {
		h.handleClaudeNonStream(ctx, w, kiroPayload, req.Model, thinking, thinkingResponseOpts, estimatedInputTokens, cacheProfile, apiKeyID)
	}
}

// handleClaudeStream Claude 流式响应
func (h *Handler) handleClaudeStream(ctx context.Context, w http.ResponseWriter, payload *KiroPayload, model string, thinking bool, thinkingOpts claudeThinkingResponseOptions, estimatedInputTokens int, cacheProfile *promptCacheProfile, apiKeyID string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.sendClaudeError(w, 500, "api_error", "Streaming not supported")
		return
	}
	hb := startSSEHeartbeat(ctx, w, flusher, claudePingFrame)
	defer hb.Stop()
	w, flusher = hb, hb

	// 获取 thinking 输出格式配置
	thinkingFormat := thinkingOpts.Format

	msgID := "msg_" + uuid.New().String()
	startInputTokens := estimatedInputTokens
	messageStarted := false
	var messageStartUsage promptCacheUsage

	ensureMessageStart := func() {
		if messageStarted {
			return
		}
		h.sendSSE(w, flusher, "message_start", map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":            msgID,
				"type":          "message",
				"role":          "assistant",
				"content":       []interface{}{},
				"model":         echoModel(ctx, model),
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage":         buildClaudeUsageMap(startInputTokens, 0, messageStartUsage, cacheProfile != nil),
			},
		})
		messageStarted = true
	}

	attempt := func(a *upstreamAttempt) upstreamAttemptRun {
		model = a.model
		cacheUsage := a.cacheUsage
		messageStartUsage = cacheUsage

		var inputTokens, outputTokens int
		var credits float64
		var realInputTokens int
		var toolUses []KiroToolUse
		var upstreamStopReason string
		var nextContentIndex int
		var rawContentBuilder strings.Builder
		var rawThinkingBuilder strings.Builder
		activeBlockIndex := -1
		activeBlockType := ""

		closeActiveBlock := func() {
			if activeBlockIndex < 0 {
				return
			}
			if activeBlockType == "thinking" {
				// Anthropic streams end every thinking block with its
				// signature (opaque here, see reasoning_output.go).
				h.sendSSE(w, flusher, "content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": activeBlockIndex,
					"delta": map[string]string{"type": "signature_delta", "signature": thinkingSignature},
				})
			}
			h.sendSSE(w, flusher, "content_block_stop", map[string]interface{}{
				"type":  "content_block_stop",
				"index": activeBlockIndex,
			})
			activeBlockIndex = -1
			activeBlockType = ""
		}

		startContentBlock := func(blockType string) {
			if activeBlockType == blockType {
				return
			}
			ensureMessageStart()
			closeActiveBlock()

			idx := nextContentIndex
			nextContentIndex++

			if blockType == "thinking" {
				h.sendSSE(w, flusher, "content_block_start", map[string]interface{}{
					"type":  "content_block_start",
					"index": idx,
					"content_block": map[string]string{
						"type":      "thinking",
						"thinking":  "",
						"signature": "",
					},
				})
			} else {
				h.sendSSE(w, flusher, "content_block_start", map[string]interface{}{
					"type":  "content_block_start",
					"index": idx,
					"content_block": map[string]string{
						"type": "text",
						"text": "",
					},
				})
			}

			activeBlockIndex = idx
			activeBlockType = blockType
		}

		var textBuffer string
		var inThinkingBlock bool
		var dropTagThinking bool
		var thinkingSource thinkingStreamSource
		var thinkingStarted bool
		var eventThinkingOpen bool

		// Stop sequences apply to answer text only. A match cuts the text and
		// cancels the upstream call; stopBypass lets held-back text out at the
		// end of a text run.
		stopper := newStopSequenceMatcher(stopSequencesFrom(ctx))
		cancelUpstream := a.cancel
		stopBypass := false

		sendText := func(text string, thinkingState int) {
			if thinkingState == 0 {
				if stopper != nil && !stopBypass {
					text = stopper.Push(text)
					if stopper.Done() {
						cancelUpstream()
					}
				}
				if text == "" {
					return
				}
				startContentBlock("text")
				writeClaudeDelta(w, flusher, activeBlockIndex, "text", text)
				return
			}

			if !thinking {
				return
			}

			switch thinkingFormat {
			case "think":
				var outputText string
				switch thinkingState {
				case 1:
					outputText = "<think>" + text
				case 2:
					outputText = text
				case 3:
					outputText = text + "</think>"
				}
				if outputText == "" {
					return
				}
				startContentBlock("text")
				writeClaudeDelta(w, flusher, activeBlockIndex, "text", outputText)
			case "reasoning_content":
				if text == "" {
					return
				}
				startContentBlock("text")
				writeClaudeDelta(w, flusher, activeBlockIndex, "text", text)
			default:
				if thinkingOpts.OmitDisplay {
					if thinkingState == 1 {
						startContentBlock("thinking")
						return
					}
					if thinkingState == 3 {
						if activeBlockType != "thinking" {
							startContentBlock("thinking")
						}
						closeActiveBlock()
					}
					return
				}
				if thinkingState == 3 && text == "" {
					if activeBlockType == "thinking" {
						closeActiveBlock()
					}
					return
				}
				if text != "" {
					startContentBlock("thinking")
					writeClaudeDelta(w, flusher, activeBlockIndex, "thinking", text)
				}
				if thinkingState == 3 && activeBlockType == "thinking" {
					closeActiveBlock()
				}
			}
		}

		processClaudeText := func(text string, isThinking bool, forceFlush bool) {
			if isThinking && !thinking {
				return
			}

			if isThinking {
				if !allowReasoningSource(&thinkingSource) {
					return
				}
				if !thinkingStarted {
					sendText(text, 1)
					thinkingStarted = true
					eventThinkingOpen = true
				} else {
					sendText(text, 2)
				}
				return
			}

			if eventThinkingOpen {
				sendText("", 3)
				eventThinkingOpen = false
				thinkingStarted = false
			}

			textBuffer += text

			for {
				if !inThinkingBlock {
					thinkingStart := strings.Index(textBuffer, "<thinking>")
					if thinkingStart != -1 {
						if thinkingStart > 0 {
							sendText(textBuffer[:thinkingStart], 0)
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
							sendText(head, 0)
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
								sendText(content, 1)
								sendText("", 3)
							} else {
								sendText(content, 3)
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
									sendText(textBuffer, 1)
									sendText("", 3)
								} else {
									sendText(textBuffer, 3)
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
										sendText(head, 1)
										thinkingStarted = true
									} else {
										sendText(head, 2)
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
				sendText(rest, 0)
				stopBypass = false
			}
		}

		callback := &KiroStreamCallback{
			OnText: func(text string, isThinking bool) {
				if text == "" || stopper.Done() {
					return
				}
				if isThinking {
					rawThinkingBuilder.WriteString(text)
				} else {
					rawContentBuilder.WriteString(text)
				}
				processClaudeText(text, isThinking, false)
			},
			OnToolUse: func(tu KiroToolUse) {
				if stopper.Done() {
					return
				}
				processClaudeText("", false, true)
				flushStopText()
				if stopper.Done() {
					return
				}
				rawContentBuilder.WriteString(tu.Name)
				if b, err := json.Marshal(tu.Input); err == nil {
					rawContentBuilder.Write(b)
				}

				toolUses = append(toolUses, tu)
				ensureMessageStart()
				closeActiveBlock()

				idx := nextContentIndex
				nextContentIndex++

				h.sendSSE(w, flusher, "content_block_start", map[string]interface{}{
					"type":  "content_block_start",
					"index": idx,
					"content_block": map[string]interface{}{
						"type":  "tool_use",
						"id":    tu.ToolUseID,
						"name":  tu.Name,
						"input": map[string]interface{}{},
					},
				})

				inputJSON, _ := json.Marshal(tu.Input)
				h.sendSSE(w, flusher, "content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": idx,
					"delta": map[string]interface{}{
						"type":         "input_json_delta",
						"partial_json": string(inputJSON),
					},
				})

				h.sendSSE(w, flusher, "content_block_stop", map[string]interface{}{
					"type":  "content_block_stop",
					"index": idx,
				})
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
			OnStopReason: func(reason string) {
				upstreamStopReason = reason
			},
		}

		measure := func() (int, int, string, bool) {
			return rawContentBuilder.Len(), len(toolUses), upstreamStopReason, rawThinkingBuilder.Len() > 0
		}

		// A same-account retry only happens while nothing has been flushed, so
		// SSE block indices are still at their initial values and need no
		// rollback. What must be cleared is every accumulator plus the thinking
		// tag parser state: processClaudeText buffers up to 50 runes before
		// flushing, so a short truncated attempt can leave a partial tag behind
		// that would otherwise be prefixed onto the retry's first chunk.
		reset := func() {
			rawContentBuilder.Reset()
			rawThinkingBuilder.Reset()
			toolUses = nil
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

		finish := func() upstreamUsage {
			processClaudeText("", false, true)
			flushStopText()
			if eventThinkingOpen {
				sendText("", 3)
			}
			closeActiveBlock()

			inputTokens = resolveInputTokens(realInputTokens, inputTokens, estimatedInputTokens)
			outputContent, extractedReasoning := extractThinkingFromContent(rawContentBuilder.String())
			thinkingOutput := rawThinkingBuilder.String()
			if thinking && thinkingOutput == "" && extractedReasoning != "" {
				thinkingOutput = extractedReasoning
			}
			if !thinking {
				thinkingOutput = ""
			}
			outputTokens = estimateClaudeOutputTokens(outputContent, thinkingOutput, toolUses)
			return upstreamUsage{inputTokens: inputTokens, outputTokens: outputTokens, credits: credits}
		}

		respond := func(upstreamUsage) {
			stopReason := mapClaudeStopReason(upstreamStopReason, len(toolUses))
			var stopSequence interface{}
			if stopper.Done() {
				stopReason = "stop_sequence"
				stopSequence = stopper.Matched()
			}

			ensureMessageStart()
			h.sendSSE(w, flusher, "message_delta", map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]interface{}{
					"stop_reason":   stopReason,
					"stop_sequence": stopSequence,
				},
				"usage": buildClaudeUsageMap(inputTokens, outputTokens, cacheUsage, cacheProfile != nil),
			})

			h.sendSSE(w, flusher, "message_stop", map[string]interface{}{
				"type": "message_stop",
			})
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
		endpoint:     "claude",
		payload:      payload,
		model:        model,
		apiKeyID:     apiKeyID,
		cacheProfile: cacheProfile,
		started:      func() bool { return messageStarted },
		attempt:      attempt,
		failStarted: func(a *upstreamAttempt, err error) {
			h.sendSSE(w, flusher, "error", claudeStreamErrorEvent(err, estimatedInputTokens, a.model))
		},
		failNoAccounts: func() {
			if hb.Committed() {
				h.sendSSE(w, flusher, "error", map[string]interface{}{
					"type":  "error",
					"error": map[string]string{"type": "overloaded_error", "message": noAvailableAccountsMessage},
				})
				return
			}
			h.sendClaudeNoAccounts(w)
		},
		fail: func(model string, err error) {
			if hb.Committed() {
				// A heartbeat already sent the 200 status line.
				h.sendSSE(w, flusher, "error", claudeStreamErrorEvent(err, estimatedInputTokens, model))
				return
			}
			h.sendClaudeUpstreamError(w, err, estimatedInputTokens, model)
		},
	})
}

func (h *Handler) sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	writeSSEEvent(w, flusher, event, jsonData)
}

// handleClaudeNonStream Claude 非流式响应
func (h *Handler) handleClaudeNonStream(ctx context.Context, w http.ResponseWriter, payload *KiroPayload, model string, thinking bool, thinkingOpts claudeThinkingResponseOptions, estimatedInputTokens int, cacheProfile *promptCacheProfile, apiKeyID string) {
	attempt := func(a *upstreamAttempt) upstreamAttemptRun {
		model = a.model
		cacheUsage := a.cacheUsage

		var content string
		var thinkingContent string
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
					thinkingContent += text
				} else {
					content += text
					stopWatch.Observe(content, len(text))
				}
			},
			OnToolUse: func(tu KiroToolUse) {
				if stopWatch.Hit() {
					return
				}
				toolUses = append(toolUses, tu)
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
			OnStopReason: func(reason string) {
				upstreamStopReason = reason
			},
		}

		measure := func() (int, int, string, bool) {
			return len(content), len(toolUses), upstreamStopReason, thinkingContent != ""
		}

		reset := func() {
			content = ""
			thinkingContent = ""
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
		var finalContent, rawThinkingContent, matchedStop string
		finish := func() upstreamUsage {
			var extractedReasoning string
			finalContent, extractedReasoning = extractThinkingFromContent(content)
			finalContent, toolUses, matchedStop = stopWatch.Apply(finalContent, toolUses)
			rawThinkingContent = thinkingContent
			if thinking && rawThinkingContent == "" && extractedReasoning != "" {
				rawThinkingContent = extractedReasoning
			}
			if !thinking {
				rawThinkingContent = ""
			}

			inputTokens = resolveInputTokens(realInputTokens, inputTokens, estimatedInputTokens)
			outputTokens = estimateClaudeOutputTokens(finalContent, rawThinkingContent, toolUses)
			return upstreamUsage{inputTokens: inputTokens, outputTokens: outputTokens, credits: credits}
		}

		respond := func(upstreamUsage) {
			thinkingFormat := thinkingOpts.Format
			responseThinkingContent := rawThinkingContent
			includeEmptyThinkingBlock := thinking && thinkingOpts.OmitDisplay && rawThinkingContent != ""
			if includeEmptyThinkingBlock {
				responseThinkingContent = ""
			}

			if thinking && responseThinkingContent != "" {
				switch thinkingFormat {
				case "think":
					finalContent = "<think>" + responseThinkingContent + "</think>" + finalContent
					responseThinkingContent = ""
				case "reasoning_content":
					finalContent = responseThinkingContent + finalContent
					responseThinkingContent = ""
				default:
				}
			}

			resp := KiroToClaudeResponse(finalContent, responseThinkingContent, includeEmptyThinkingBlock, toolUses, inputTokens, outputTokens, echoModel(ctx, model), upstreamStopReason)
			if matchedStop != "" {
				resp.StopReason = "stop_sequence"
				resp.StopSequence = &matchedStop
			}
			resp.Usage.InputTokens = billedClaudeInputTokens(inputTokens, cacheUsage)
			resp.Usage.CacheCreationInputTokens = cacheUsage.CacheCreationInputTokens
			resp.Usage.CacheReadInputTokens = cacheUsage.CacheReadInputTokens
			if cacheProfile != nil {
				resp.Usage.CacheCreation = &ClaudeCacheCreationUsage{
					Ephemeral5mInputTokens: cacheUsage.CacheCreation5mInputTokens,
					Ephemeral1hInputTokens: cacheUsage.CacheCreation1hInputTokens,
				}
			}
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
		endpoint:       "claude",
		payload:        payload,
		model:          model,
		apiKeyID:       apiKeyID,
		cacheProfile:   cacheProfile,
		attempt:        attempt,
		failNoAccounts: func() { h.sendClaudeNoAccounts(w) },
		fail: func(model string, err error) {
			h.sendClaudeUpstreamError(w, err, estimatedInputTokens, model)
		},
	})
}

func (h *Handler) sendClaudeError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]string{
			"type":    errType,
			"message": message,
		},
	})
}
