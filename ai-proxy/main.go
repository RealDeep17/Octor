package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	geminiAPIKey string
	geminiModel  string
	proxyPort    int
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {
	geminiAPIKey = os.Getenv("GEMINI_API_KEY")
	geminiModel = os.Getenv("AI_RECOMMENDATIONS_MODEL")
	if geminiModel == "" {
		geminiModel = "gemini-3.1-flash-lite"
	}

	if geminiAPIKey == "" {
		log.Fatal("❌ GEMINI_API_KEY is not set. Check your environment.")
	}

	portStr := os.Getenv("ANTHROPIC_PROXY_PORT")
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err == nil {
			proxyPort = p
		}
	}
	if proxyPort == 0 {
		proxyPort = 3456
	}

	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/v1/messages", handleMessages)

	log.Printf("\n🔀 Anthropic → Gemini proxy (Go)")
	log.Printf("    Listening : http://127.0.0.1:%d", proxyPort)
	log.Printf("    Model     : %s", geminiModel)
	log.Printf("    API key   : ✓ set\n")

	addr := fmt.Sprintf("127.0.0.1:%d", proxyPort)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatalf("❌ Server error: %v", err)
	}
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"model": geminiModel,
	})
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Use POST /v1/messages",
		})
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var anthropicReq map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &anthropicReq); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Invalid JSON",
		})
		return
	}

	originalModel, _ := anthropicReq["model"].(string)
	if originalModel == "" {
		originalModel = "claude-haiku-4-5-20251001"
	}

	geminiBody := buildGeminiBody(anthropicReq)

	isStream := false
	if st, ok := anthropicReq["stream"].(bool); ok {
		isStream = st
	}

	mode := "sync"
	if isStream {
		mode = "stream"
	}
	log.Printf("[proxy] %s → %s  (was: %s)", mode, geminiModel, originalModel)

	if isStream {
		handleStream(w, geminiBody, originalModel)
	} else {
		handleSync(w, geminiBody, originalModel)
	}
}

func handleSync(w http.ResponseWriter, geminiBody map[string]interface{}, originalModel string) {
	payload, err := json.Marshal(geminiBody)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", geminiModel, geminiAPIKey)
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[proxy] Gemini error: %s", string(respBytes))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": string(respBytes),
			},
		})
		return
	}

	var geminiData map[string]interface{}
	if err := json.Unmarshal(respBytes, &geminiData); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to parse Gemini response: "+err.Error())
		return
	}

	anthropicResp := toAnthropicResponse(geminiData, originalModel)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(anthropicResp)
}

func handleStream(w http.ResponseWriter, geminiBody map[string]interface{}, originalModel string) {
	payload, err := json.Marshal(geminiBody)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming unsupported by client")
		return
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", geminiModel, geminiAPIKey)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	msgID := fmt.Sprintf("msg_proxy_%d", time.Now().UnixNano()/int64(time.Millisecond))

	// ── Open the Anthropic SSE envelope ──────────────────────────────────────
	sendSSE(w, flusher, "message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":      msgID,
			"type":    "message",
			"role":    "assistant",
			"model":   originalModel,
			"content": []interface{}{},
			"stop_reason": nil,
			"usage": map[string]interface{}{
				"input_tokens":                0,
				"output_tokens":               0,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     0,
			},
		},
	})
	sendSSE(w, flusher, "content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	})
	sendSSE(w, flusher, "ping", map[string]interface{}{
		"type": "ping",
	})

	reader := bufio.NewReader(resp.Body)
	inputTokens := 0
	outputTokens := 0

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("[proxy] Stream read error: %v", err)
			break
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		jsonStr := strings.TrimPrefix(line, "data: ")
		if jsonStr == "" || jsonStr == "[DONE]" {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			continue
		}

		candidates, _ := data["candidates"].([]interface{})
		if len(candidates) > 0 {
			if candidate, ok := candidates[0].(map[string]interface{}); ok {
				if content, ok := candidate["content"].(map[string]interface{}); ok {
					if parts, ok := content["parts"].([]interface{}); ok {
						for _, p := range parts {
							if pMap, ok := p.(map[string]interface{}); ok {
								if text, ok := pMap["text"].(string); ok && text != "" {
									sendSSE(w, flusher, "content_block_delta", map[string]interface{}{
										"type":  "content_block_delta",
										"index": 0,
										"delta": map[string]interface{}{
											"type": "text_delta",
											"text": text,
										},
									})
								}
							}
						}
					}
				}
			}
		}

		if usageMetadata, ok := data["usageMetadata"].(map[string]interface{}); ok {
			if pt, ok := usageMetadata["promptTokenCount"].(float64); ok {
				inputTokens = int(pt)
			}
			if ct, ok := usageMetadata["candidatesTokenCount"].(float64); ok {
				outputTokens = int(ct)
			}
		}
	}

	sendSSE(w, flusher, "content_block_stop", map[string]interface{}{
		"type":  "content_block_stop",
		"index": 0,
	})
	sendSSE(w, flusher, "message_delta", map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{
			"output_tokens":               outputTokens,
			"input_tokens":                inputTokens,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
		},
	})
	sendSSE(w, flusher, "message_stop", map[string]interface{}{
		"type": "message_stop",
	})
}

func sendSSE(w io.Writer, flusher http.Flusher, event string, data interface{}) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
	flusher.Flush()
}

func writeError(w http.ResponseWriter, status int, message string) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "internal",
			"message": message,
		},
	})
}

// Helper conversion functions

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func convertSchema(schema interface{}) interface{} {
	m, ok := schema.(map[string]interface{})
	if !ok {
		return map[string]interface{}{"type": "STRING"}
	}
	out := make(map[string]interface{})
	if t, ok := m["type"]; ok {
		if typeVal, ok := t.(string); ok {
			if typeVal == "integer" {
				out["type"] = "NUMBER"
			} else {
				out["type"] = strings.ToUpper(typeVal)
			}
		} else if typeVals, ok := t.([]interface{}); ok {
			hasNull := false
			var primaryType string
			for _, val := range typeVals {
				if str, ok := val.(string); ok {
					if str == "null" {
						hasNull = true
					} else if primaryType == "" {
						primaryType = str
					}
				}
			}
			if hasNull {
				out["nullable"] = true
			}
			if primaryType == "" {
				primaryType = "string"
			}
			if primaryType == "integer" {
				out["type"] = "NUMBER"
			} else {
				out["type"] = strings.ToUpper(primaryType)
			}
		}
	}
	for _, key := range []string{"description", "enum", "minItems", "maxItems", "maxLength", "required"} {
		if val, ok := m[key]; ok {
			out[key] = val
		}
	}
	if props, ok := m["properties"]; ok {
		if propsMap, ok := props.(map[string]interface{}); ok {
			newProps := make(map[string]interface{})
			for k, v := range propsMap {
				newProps[k] = convertSchema(v)
			}
			out["properties"] = newProps
		}
	}
	if items, ok := m["items"]; ok {
		out["items"] = convertSchema(items)
	}
	return out
}

func toGeminiContents(messages []interface{}) []interface{} {
	out := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		mMap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		roleVal, _ := mMap["role"].(string)
		role := "user"
		if roleVal == "assistant" {
			role = "model"
		}
		text := ""
		content := mMap["content"]
		if contentStr, ok := content.(string); ok {
			text = contentStr
		} else if contentArr, ok := content.([]interface{}); ok {
			var parts []string
			for _, b := range contentArr {
				if bMap, ok := b.(map[string]interface{}); ok {
					typeStr, _ := bMap["type"].(string)
					if typeStr == "text" {
						txt, _ := bMap["text"].(string)
						parts = append(parts, txt)
					}
				}
			}
			text = strings.Join(parts, "\n")
		}
		out = append(out, map[string]interface{}{
			"role": role,
			"parts": []interface{}{
				map[string]interface{}{"text": text},
			},
		})
	}
	return out
}

func extractSystemText(system interface{}) string {
	if system == nil {
		return ""
	}
	if str, ok := system.(string); ok {
		return str
	}
	if arr, ok := system.([]interface{}); ok {
		var parts []string
		for _, b := range arr {
			if bMap, ok := b.(map[string]interface{}); ok {
				typeStr, _ := bMap["type"].(string)
				if typeStr == "text" {
					txt, _ := bMap["text"].(string)
					parts = append(parts, txt)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func toFunctionDeclarations(tools []interface{}) []interface{} {
	if len(tools) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		tMap, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := tMap["name"].(string)
		description, _ := tMap["description"].(string)

		var properties interface{} = map[string]interface{}{}
		var required interface{} = []interface{}{}

		if schema, ok := tMap["input_schema"].(map[string]interface{}); ok {
			if req, ok := schema["required"]; ok {
				required = req
			}
			if props, ok := schema["properties"].(map[string]interface{}); ok {
				newProps := make(map[string]interface{})
				for k, v := range props {
					newProps[k] = convertSchema(v)
				}
				properties = newProps
			}
		}

		out = append(out, map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters": map[string]interface{}{
				"type":       "OBJECT",
				"properties": properties,
				"required":   required,
			},
		})
	}
	return out
}

func toToolConfig(toolChoice interface{}) interface{} {
	if toolChoice == nil {
		return nil
	}
	tcMap, ok := toolChoice.(map[string]interface{})
	if !ok {
		return nil
	}
	tType, _ := tcMap["type"].(string)
	if tType == "auto" {
		return map[string]interface{}{
			"functionCallingConfig": map[string]interface{}{"mode": "AUTO"},
		}
	}
	if tType == "any" {
		return map[string]interface{}{
			"functionCallingConfig": map[string]interface{}{"mode": "ANY"},
		}
	}
	if tType == "tool" {
		name, _ := tcMap["name"].(string)
		if name != "" {
			return map[string]interface{}{
				"functionCallingConfig": map[string]interface{}{
					"mode":                 "ANY",
					"allowedFunctionNames": []string{name},
				},
			}
		}
	}
	return map[string]interface{}{
		"functionCallingConfig": map[string]interface{}{"mode": "AUTO"},
	}
}

func buildGeminiBody(req map[string]interface{}) map[string]interface{} {
	maxTokens := 1024
	if mt, ok := req["max_tokens"].(float64); ok {
		maxTokens = int(mt)
	}

	messages, _ := req["messages"].([]interface{})

	genConfig := map[string]interface{}{
		"maxOutputTokens": maxTokens,
	}
	if temp, ok := req["temperature"]; ok {
		genConfig["temperature"] = temp
	}
	if stopSeq, ok := req["stop_sequences"]; ok {
		genConfig["stopSequences"] = stopSeq
	}

	body := map[string]interface{}{
		"contents":         toGeminiContents(messages),
		"generationConfig": genConfig,
	}

	sys := extractSystemText(req["system"])
	if sys != "" {
		body["systemInstruction"] = map[string]interface{}{
			"parts": []interface{}{
				map[string]interface{}{"text": sys},
			},
		}
	}

	if toolsVal, ok := req["tools"].([]interface{}); ok && len(toolsVal) > 0 {
		fns := toFunctionDeclarations(toolsVal)
		if fns != nil {
			body["tools"] = []interface{}{
				map[string]interface{}{"functionDeclarations": fns},
			}
			if tc := toToolConfig(req["tool_choice"]); tc != nil {
				body["toolConfig"] = tc
			}
		}
	}
	return body
}

func toAnthropicResponse(gemini map[string]interface{}, originalModel string) map[string]interface{} {
	candidates, _ := gemini["candidates"].([]interface{})
	var candidate map[string]interface{}
	if len(candidates) > 0 {
		candidate, _ = candidates[0].(map[string]interface{})
	}
	if candidate == nil {
		candidate = make(map[string]interface{})
	}

	var parts []interface{}
	if content, ok := candidate["content"].(map[string]interface{}); ok {
		parts, _ = content["parts"].([]interface{})
	}

	contentOut := make([]interface{}, 0)
	stopReason := "end_turn"

	for _, p := range parts {
		pMap, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		if txt, ok := pMap["text"].(string); ok && txt != "" {
			contentOut = append(contentOut, map[string]interface{}{
				"type": "text",
				"text": txt,
			})
		} else if fnCall, ok := pMap["functionCall"].(map[string]interface{}); ok {
			name, _ := fnCall["name"].(string)
			args := fnCall["args"]
			if args == nil {
				args = map[string]interface{}{}
			}

			toolID := fmt.Sprintf("toolu_%d_%s", time.Now().UnixNano()/int64(time.Millisecond), randString(6))
			contentOut = append(contentOut, map[string]interface{}{
				"type":  "tool_use",
				"id":    toolID,
				"name":  name,
				"input": args,
			})
			stopReason = "tool_use"
		}
	}

	if finishReason, ok := candidate["finishReason"].(string); ok && finishReason == "MAX_TOKENS" {
		stopReason = "max_tokens"
	}

	if len(contentOut) == 0 {
		contentOut = append(contentOut, map[string]interface{}{
			"type": "text",
			"text": "",
		})
	}

	usage := map[string]interface{}{
		"input_tokens":                0,
		"output_tokens":               0,
		"cache_creation_input_tokens": 0,
		"cache_read_input_tokens":     0,
	}
	if usageMetadata, ok := gemini["usageMetadata"].(map[string]interface{}); ok {
		if pt, ok := usageMetadata["promptTokenCount"].(float64); ok {
			usage["input_tokens"] = int(pt)
		}
		if ct, ok := usageMetadata["candidatesTokenCount"].(float64); ok {
			usage["output_tokens"] = int(ct)
		}
	}

	return map[string]interface{}{
		"id":            fmt.Sprintf("msg_proxy_%d", time.Now().UnixNano()/int64(time.Millisecond)),
		"type":          "message",
		"role":          "assistant",
		"model":         originalModel,
		"content":       contentOut,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         usage,
	}
}
