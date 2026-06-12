#!/usr/bin/env node
/**
 * Anthropic → Gemini Proxy
 *
 * Sits between the octor web-ui (which uses the Anthropic Go SDK) and the
 * Google Gemini API. Translates every POST /v1/messages request – including
 * streaming, tool-use (for chips), and prompt-caching beta headers – so the
 * Go code never needs to know it is talking to Gemini.
 *
 * Required env vars (loaded from custom.env):
 *   GEMINI_API_KEY              – your Google AI Studio key
 *   AI_RECOMMENDATIONS_MODEL   – model string (default: gemini-3.1-flash-lite)
 *
 * Optional:
 *   ANTHROPIC_PROXY_PORT        – port to listen on (default: 3456)
 */

'use strict';

const http  = require('http');
const https = require('https');

const PORT         = Number(process.env.ANTHROPIC_PROXY_PORT) || 3456;
const GEMINI_KEY   = process.env.GEMINI_API_KEY || '';
const GEMINI_MODEL = process.env.AI_RECOMMENDATIONS_MODEL || 'gemini-3.1-flash-lite';

if (!GEMINI_KEY) {
  console.error('❌  GEMINI_API_KEY is not set. Check custom.env.');
  process.exit(1);
}

// ─── Schema helpers ────────────────────────────────────────────────────────

function convertSchema(schema) {
  if (!schema || typeof schema !== 'object') return { type: 'STRING' };
  const out = {};
  if (schema.type) {
    let typeVal = schema.type;
    if (Array.isArray(typeVal)) {
      if (typeVal.includes('null')) {
        out.nullable = true;
      }
      typeVal = typeVal.find(t => t !== 'null') || 'string';
    }
    // Gemini doesn't have INTEGER; use NUMBER
    out.type = typeVal === 'integer' ? 'NUMBER' : String(typeVal).toUpperCase();
  }
  if (schema.description) out.description = schema.description;
  if (schema.enum)         out.enum        = schema.enum;
  if (schema.minItems)     out.minItems    = schema.minItems;
  if (schema.maxItems)     out.maxItems    = schema.maxItems;
  if (schema.maxLength)    out.maxLength   = schema.maxLength;
  if (schema.required)     out.required    = schema.required;
  if (schema.properties) {
    out.properties = {};
    for (const [k, v] of Object.entries(schema.properties))
      out.properties[k] = convertSchema(v);
  }
  if (schema.items) out.items = convertSchema(schema.items);
  return out;
}

// ─── Request translation: Anthropic → Gemini ──────────────────────────────

function extractSystemText(system) {
  if (!system) return '';
  if (typeof system === 'string') return system;
  if (Array.isArray(system))
    return system.filter(b => b.type === 'text').map(b => b.text).join('\n');
  return '';
}

function toGeminiContents(messages) {
  return (messages || []).map(msg => {
    const role = msg.role === 'assistant' ? 'model' : 'user';
    let text = '';
    if (typeof msg.content === 'string') {
      text = msg.content;
    } else if (Array.isArray(msg.content)) {
      text = msg.content.filter(b => b.type === 'text').map(b => b.text).join('\n');
    }
    return { role, parts: [{ text }] };
  });
}

function toFunctionDeclarations(tools) {
  if (!tools || !tools.length) return null;
  return tools.map(t => ({
    name:        t.name,
    description: t.description || '',
    parameters: {
      type:       'OBJECT',
      properties: t.input_schema?.properties
        ? Object.fromEntries(
            Object.entries(t.input_schema.properties).map(([k, v]) => [k, convertSchema(v)])
          )
        : {},
      required: t.input_schema?.required || [],
    },
  }));
}

function toToolConfig(toolChoice) {
  if (!toolChoice) return null;
  if (toolChoice.type === 'auto')                return { functionCallingConfig: { mode: 'AUTO' } };
  if (toolChoice.type === 'any')                 return { functionCallingConfig: { mode: 'ANY'  } };
  if (toolChoice.type === 'tool' && toolChoice.name)
    return { functionCallingConfig: { mode: 'ANY', allowedFunctionNames: [toolChoice.name] } };
  return { functionCallingConfig: { mode: 'AUTO' } };
}

function buildGeminiBody(req) {
  const body = {
    contents:         toGeminiContents(req.messages),
    generationConfig: { maxOutputTokens: req.max_tokens || 1024 },
  };
  if (req.temperature !== undefined) body.generationConfig.temperature = req.temperature;
  if (req.stop_sequences?.length)    body.generationConfig.stopSequences = req.stop_sequences;

  const sys = extractSystemText(req.system);
  if (sys) body.systemInstruction = { parts: [{ text: sys }] };

  const fns = toFunctionDeclarations(req.tools);
  if (fns) {
    body.tools = [{ functionDeclarations: fns }];
    const tc = toToolConfig(req.tool_choice);
    if (tc) body.toolConfig = tc;
  }
  return body;
}

// ─── Response translation: Gemini → Anthropic ─────────────────────────────

function toAnthropicResponse(gemini, originalModel) {
  const candidate = gemini.candidates?.[0] || {};
  const parts     = candidate.content?.parts || [];
  const content   = [];
  let   stopReason = 'end_turn';

  for (const part of parts) {
    if (part.text) {
      content.push({ type: 'text', text: part.text });
    } else if (part.functionCall) {
      content.push({
        type:  'tool_use',
        id:    `toolu_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
        name:  part.functionCall.name,
        input: part.functionCall.args || {},
      });
      stopReason = 'tool_use';
    }
  }

  if (candidate.finishReason === 'MAX_TOKENS') stopReason = 'max_tokens';

  const usage = gemini.usageMetadata || {};
  return {
    id:            `msg_proxy_${Date.now()}`,
    type:          'message',
    role:          'assistant',
    model:         originalModel,
    content:       content.length ? content : [{ type: 'text', text: '' }],
    stop_reason:   stopReason,
    stop_sequence: null,
    usage: {
      input_tokens:                  usage.promptTokenCount      || 0,
      output_tokens:                 usage.candidatesTokenCount  || 0,
      cache_creation_input_tokens:   0,
      cache_read_input_tokens:       0,
    },
  };
}

// ─── Gemini API calls ──────────────────────────────────────────────────────

function geminiRequest(path, bodyObj) {
  return new Promise((resolve, reject) => {
    const payload = JSON.stringify(bodyObj);
    const req = https.request({
      hostname: 'generativelanguage.googleapis.com',
      path:     `${path}?key=${GEMINI_KEY}`,
      method:   'POST',
      headers:  { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(payload) },
    }, res => {
      let buf = '';
      res.on('data', c => (buf += c));
      res.on('end', () => {
        try   { resolve({ status: res.statusCode, body: JSON.parse(buf) }); }
        catch { reject(new Error('Gemini non-JSON: ' + buf.slice(0, 300))); }
      });
    });
    req.on('error', reject);
    req.write(payload);
    req.end();
  });
}

function sendSSE(res, event, data) {
  res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
}

function handleStream(geminiBody, clientRes, originalModel) {
  const path    = `/v1beta/models/${GEMINI_MODEL}:streamGenerateContent?alt=sse&key=${GEMINI_KEY}`;
  const payload = JSON.stringify(geminiBody);
  const msgId   = `msg_proxy_${Date.now()}`;

  clientRes.writeHead(200, {
    'Content-Type':                'text/event-stream',
    'Cache-Control':               'no-cache',
    'Connection':                  'keep-alive',
    'Access-Control-Allow-Origin': '*',
  });

  // ── Open the Anthropic SSE envelope ──────────────────────────────────────
  sendSSE(clientRes, 'message_start', {
    type: 'message_start',
    message: {
      id: msgId, type: 'message', role: 'assistant', model: originalModel,
      content: [], stop_reason: null,
      usage: { input_tokens: 0, output_tokens: 0,
               cache_creation_input_tokens: 0, cache_read_input_tokens: 0 },
    },
  });
  sendSSE(clientRes, 'content_block_start', {
    type: 'content_block_start', index: 0,
    content_block: { type: 'text', text: '' },
  });
  sendSSE(clientRes, 'ping', { type: 'ping' });

  // ── Open HTTPS connection to Gemini ──────────────────────────────────────
  const req = https.request({
    hostname: 'generativelanguage.googleapis.com',
    path,
    method:   'POST',
    headers:  { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(payload) },
  }, geminiRes => {
    let buf = '';
    let inputTokens  = 0;
    let outputTokens = 0;

    geminiRes.on('data', chunk => {
      buf += chunk.toString();
      const lines = buf.split('\n');
      buf = lines.pop(); // keep the partial last line

      for (const line of lines) {
        if (!line.startsWith('data: ')) continue;
        const json = line.slice(6).trim();
        if (!json || json === '[DONE]') continue;
        try {
          const data  = JSON.parse(json);
          const parts = data.candidates?.[0]?.content?.parts || [];
          for (const part of parts) {
            if (part.text) {
              sendSSE(clientRes, 'content_block_delta', {
                type: 'content_block_delta', index: 0,
                delta: { type: 'text_delta', text: part.text },
              });
            }
          }
          if (data.usageMetadata) {
            inputTokens  = data.usageMetadata.promptTokenCount     || inputTokens;
            outputTokens = data.usageMetadata.candidatesTokenCount || outputTokens;
          }
        } catch { /* skip malformed lines */ }
      }
    });

    geminiRes.on('end', () => {
      sendSSE(clientRes, 'content_block_stop', { type: 'content_block_stop', index: 0 });
      sendSSE(clientRes, 'message_delta', {
        type: 'message_delta',
        delta: { stop_reason: 'end_turn', stop_sequence: null },
        usage: {
          output_tokens: outputTokens,
          input_tokens:  inputTokens,
          cache_creation_input_tokens: 0,
          cache_read_input_tokens:     0,
        },
      });
      sendSSE(clientRes, 'message_stop', { type: 'message_stop' });
      clientRes.end();
    });

    geminiRes.on('error', err => {
      console.error('[proxy] Gemini stream error:', err.message);
      clientRes.end();
    });
  });

  req.on('error', err => {
    console.error('[proxy] Gemini connect error:', err.message);
    clientRes.end();
  });
  req.write(payload);
  req.end();
}

// ─── HTTP server ──────────────────────────────────────────────────────────

const server = http.createServer((req, res) => {
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'POST, GET, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', '*');

  if (req.method === 'OPTIONS') { res.writeHead(204); res.end(); return; }

  // Health-check
  if (req.method === 'GET' && req.url === '/health') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ ok: true, model: GEMINI_MODEL }));
    return;
  }

  if (req.method !== 'POST' || req.url !== '/v1/messages') {
    res.writeHead(404, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: 'Use POST /v1/messages' }));
    return;
  }

  let raw = '';
  req.on('data', c => (raw += c));
  req.on('end', async () => {
    try {
      const body          = JSON.parse(raw);
      const originalModel = body.model || 'claude-haiku-4-5-20251001';
      const geminiBody    = buildGeminiBody(body);
      const mode          = body.stream ? 'stream' : 'sync';

      console.log(`[proxy] ${mode} → ${GEMINI_MODEL}  (was: ${originalModel})`);

      if (body.stream) {
        handleStream(geminiBody, res, originalModel);
        return;
      }

      // Non-streaming (chips tool-use path)
      const { status, body: geminiData } = await geminiRequest(
        `/v1beta/models/${GEMINI_MODEL}:generateContent`,
        geminiBody,
      );

      if (status !== 200) {
        console.error('[proxy] Gemini error:', JSON.stringify(geminiData).slice(0, 400));
        res.writeHead(status, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          type: 'error',
          error: { type: 'api_error', message: JSON.stringify(geminiData) },
        }));
        return;
      }

      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify(toAnthropicResponse(geminiData, originalModel)));

    } catch (err) {
      console.error('[proxy] Internal error:', err.message);
      if (!res.headersSent) {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ type: 'error', error: { type: 'internal', message: err.message } }));
      }
    }
  });
});

server.listen(PORT, '127.0.0.1', () => {
  console.log(`\n🔀  Anthropic → Gemini proxy`);
  console.log(`    Listening : http://127.0.0.1:${PORT}`);
  console.log(`    Model     : ${GEMINI_MODEL}`);
  console.log(`    API key   : ${GEMINI_KEY ? '✓ set' : '✗ MISSING – check GEMINI_API_KEY'}\n`);
});

process.on('SIGTERM', () => { server.close(); process.exit(0); });
process.on('SIGINT',  () => { server.close(); process.exit(0); });
