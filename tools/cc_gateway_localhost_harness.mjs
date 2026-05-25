import { createServer } from 'http'
import { createConnection } from 'net'
import { writeFileSync, mkdirSync } from 'fs'
import { startProxy } from '/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts'
import { baseConfig } from '/Users/muqihang/chelingxi_workspace/cc-gateway/tests/helpers.ts'

function listen(server) { return new Promise(resolve => server.listen(0, '127.0.0.1', resolve)) }
function addr(server) { return server.address().port }
const outDir = process.env.CC_HARNESS_OUT || '/tmp/cc-harness'
mkdirSync(outDir, { recursive: true, mode: 0o700 })
const summaries = []
const mock = createServer((req, res) => {
  const chunks = []
  req.on('data', c => chunks.push(Buffer.isBuffer(c) ? c : Buffer.from(c)))
  req.on('end', () => {
    const body = Buffer.concat(chunks).toString('utf8')
    let obj = {}
    try { obj = JSON.parse(body) } catch {}
    const headers = req.headers
    const systemText = Array.isArray(obj.system) ? JSON.stringify(obj.system) : String(obj.system || '')
    summaries.push({
      method: req.method,
      url: req.url,
      headers_names: Object.keys(headers).sort(),
      auth_shape: { authorization: headers.authorization ? String(headers.authorization).split(' ')[0] : null, x_api_key: headers['x-api-key'] ? 'present' : null },
      user_agent: headers['user-agent'] || null,
      beta: headers['anthropic-beta'] || null,
      beta_contains_context_1m: String(headers['anthropic-beta'] || '').includes('context-1m'),
      session_uuid_like: /^[0-9a-fA-F-]{36}$/.test(String(headers['x-claude-code-session-id'] || '')),
      body_size: Buffer.byteLength(body),
      body_length_bucket: Buffer.byteLength(body) <= 1024 ? '0-1024' : Buffer.byteLength(body) <= 4096 ? '1025-4096' : '4097+',
      body_digest_omitted_reason: 'plain_body_digest_forbidden',
      body_keys: obj && typeof obj === 'object' ? Object.keys(obj).sort() : [],
      model: obj.model || null,
      max_tokens: obj.max_tokens ?? null,
      tools_count: Array.isArray(obj.tools) ? obj.tools.length : 0,
      output_config_keys: obj.output_config && typeof obj.output_config === 'object' ? Object.keys(obj.output_config).sort() : [],
      has_billing_marker: systemText.includes('x-anthropic-billing-header'),
      has_cch_shape: /cch=/.test(systemText),
    })
    if (obj.stream) {
      res.writeHead(200, { 'content-type': 'text/event-stream; charset=utf-8', 'cache-control': 'no-cache' })
      const model = obj.model || 'claude-sonnet-4-6'
      const events = [
        ['message_start', { type: 'message_start', message: { id: 'msg_mock', type: 'message', role: 'assistant', model, content: [], stop_reason: null, stop_sequence: null, usage: { input_tokens: 1, output_tokens: 0 } } }],
        ['content_block_start', { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }],
        ['content_block_delta', { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'ok' } }],
        ['content_block_stop', { type: 'content_block_stop', index: 0 }],
        ['message_delta', { type: 'message_delta', delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 1 } }],
        ['message_stop', { type: 'message_stop' }],
      ]
      for (const [event, data] of events) res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`)
      res.end()
    } else {
      const data = JSON.stringify({ id: 'msg_mock', type: 'message', role: 'assistant', model: obj.model || 'claude-sonnet-4-6', content: [{ type: 'text', text: 'ok' }], stop_reason: 'end_turn', stop_sequence: null, usage: { input_tokens: 1, output_tokens: 1 } })
      res.writeHead(200, { 'content-type': 'application/json', 'content-length': Buffer.byteLength(data) })
      res.end(data)
    }
  })
})
await listen(mock)
const proxyTargets = []
const proxy = createServer()
proxy.on('connect', (req, clientSocket, head) => {
  const target = req.url || ''
  proxyTargets.push(target)
  const [host, portText] = target.split(':')
  const upstreamSocket = createConnection(Number(portText), host, () => {
    clientSocket.write('HTTP/1.1 200 Connection Established\r\n\r\n')
    if (head.length) upstreamSocket.write(head)
    upstreamSocket.pipe(clientSocket)
    clientSocket.pipe(upstreamSocket)
  })
  upstreamSocket.on('error', () => clientSocket.destroy())
})
await listen(proxy)
const config = baseConfig({
  mode: 'sub2api',
  server: { port: 0, host: '127.0.0.1', tls: { cert: '', key: '' } },
  upstream: { url: `http://127.0.0.1:${addr(mock)}` },
  auth: { gateway_token: 'ccg-token', tokens: [] },
  oauth: undefined,
  env: { ...baseConfig().env, version: '2.1.150', version_base: '2.1.150' },
  shared_pool: { upstream_mode: 'preflight', billing_cch_mode: 'sign', signing_enabled: true, signing_evidence_gates_approved: true, message_beta_profile: 'claude_code_2_1_150_subscription_1m' },
  account_identities: { 'hmac-sha256:local-harness-account': { device_id: 'b'.repeat(64), account_uuid_hash: 'hmac-sha256:local-harness-account-uuid', email_hash: 'hmac-sha256:local-harness-email', account_hash: 'hmac-sha256:local-harness-account', persona_variant: 'claude-code-2.1.150-macos-local', session_policy: 'preserve_downstream_session_id', policy_version: '2.1.150' } },
  egress_buckets: { 'bucket-a': { enabled: true, proxy_url: `http://127.0.0.1:${addr(proxy)}`, proxy_identity_hash: 'opaque:proxy-ref:v1:harness', allowed_account_ids: ['hmac-sha256:local-harness-account'] } },
  logging: { level: 'error', audit: false },
})
const gateway = startProxy(config)
await new Promise(resolve => gateway.listening ? resolve() : gateway.once('listening', resolve))
const ccGatewayUrl = serverUrl(gateway)
const mockUrl = `http://127.0.0.1:${addr(mock)}`
function serverUrl(server) { return `http://127.0.0.1:${server.address().port}` }
function writeSummary() {
  const safe = { cc_gateway_url: ccGatewayUrl, mock_url: mockUrl, proxy_connect_targets: proxyTargets, mock_request_count: summaries.length, mock_requests: summaries }
  writeFileSync(`${outDir}/cc_safe_summary.json`, JSON.stringify(safe, null, 2), { mode: 0o600 })
}
setInterval(writeSummary, 500).unref()
process.on('SIGTERM', () => { writeSummary(); gateway.close(()=>{}); mock.close(()=>{}); proxy.close(()=>{}); setTimeout(()=>process.exit(0), 100) })
console.log(JSON.stringify({ cc_gateway_url: ccGatewayUrl, out_dir: outDir, mock_url: mockUrl }))
