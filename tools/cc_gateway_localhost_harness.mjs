import { createServer } from 'http'
import { createConnection } from 'net'
import { createHmac } from 'crypto'
import { writeFileSync, mkdirSync } from 'fs'
import { join } from 'path'
import { pathToFileURL } from 'url'

const ccGatewayRoot = process.env.CC_GATEWAY_ROOT || '/Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5'
const { startProxy } = await import(pathToFileURL(join(ccGatewayRoot, 'src/proxy.ts')).href)
const { baseConfig } = await import(pathToFileURL(join(ccGatewayRoot, 'tests/helpers.ts')).href)

function listen(server) { return new Promise(resolve => server.listen(0, '127.0.0.1', resolve)) }
function addr(server) { return server.address().port }
const outDir = process.env.CC_HARNESS_OUT || '/tmp/cc-harness'
mkdirSync(outDir, { recursive: true, mode: 0o700 })
const attestationSecret = 'scheduler-hmac-material-v1-local-harness-ABCDEFGHIJKLMNOPQRSTUVWXYZ'
const internalControlToken = 'internal-control-material-v1-local-harness-ABCDEFGHIJKLMNOPQRSTUVWXYZ'
const selectedCredential = 'Bearer selected-oauth-credential-fixture'
const accountRef = `hmac-sha256:${'a'.repeat(64)}`
const credentialRef = 'opaque:credential-ref:v1:local-harness-credential'
const proxyIdentityRef = 'opaque:proxy-ref:v1:harness'
const personaProfile = 'claude_code_2_1_179_native_degraded'

const policyVersion = process.env.CC_HARNESS_POLICY_VERSION || '2.1.179'
const observedCliVersion = process.env.CC_HARNESS_OBSERVED_CLI_VERSION || policyVersion
const trustedEgressProfileRef = process.env.CC_HARNESS_EGRESS_PROFILE_REF || 'strip_attribution'
const billingShapePolicy = process.env.CC_HARNESS_BILLING_SHAPE_POLICY || 'strip'
const enableNoCchProof = process.env.CC_HARNESS_ENABLE_NO_CCH_PROOF === '1'
const enableSignedCchProof = process.env.CC_HARNESS_ENABLE_SIGNED_CCH_PROOF === '1'
const profilePolicyVersion = 'claude_code_2_1_179_cp1_degraded_v1'
const requestShapeProfileRef = 'claude_code_2_1_179_messages_streaming_tooldefs_degraded_v1'
const cacheParityProfileRef = 'claude_code_2_1_179_cache_parity_degraded_v1'
function credentialBindingHmac(rawCredential, tokenType = 'oauth') {
  return `hmac-sha256:${createHmac('sha256', attestationSecret)
    .update('formal_pool_credential_binding_v1')
    .update('\0')
    .update(tokenType)
    .update('\0')
    .update(rawCredential)
    .digest('hex')}`
}
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
      auth_presence: { authorization_present: headers.authorization !== undefined, x_api_key_present: headers['x-api-key'] !== undefined },
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
const ledgerFile = join(outDir, 'formal-pool-session-ledger.json')
const previousLedgerFile = process.env.CC_GATEWAY_FORMAL_POOL_SESSION_LEDGER_FILE
process.env.CC_GATEWAY_FORMAL_POOL_SESSION_LEDGER_FILE = ledgerFile
const config = baseConfig({
  mode: 'sub2api',
  server: { port: 0, host: '127.0.0.1', tls: { cert: '', key: '' } },
  upstream: { url: `http://127.0.0.1:${addr(mock)}` },
  auth: { gateway_token: 'ccg-token', internal_control_token: internalControlToken, tokens: [] },
  oauth: undefined,
  env: { ...baseConfig().env, version: policyVersion, version_base: policyVersion },
  shared_pool: {
    upstream_mode: 'production',
    production_upstream_enabled: true,
    billing_cch_mode: billingShapePolicy === 'signed_cch' ? 'sign' : billingShapePolicy,
    signing_enabled: true,
    signing_evidence_gates_approved: true,
    context_attestation_secret_ref: 'opaque:attestation-ref:v1:test',
    context_attestation_secret: attestationSecret,
    message_beta_profile: personaProfile,
    no_cch_2179_oracle_profile_approved: enableNoCchProof,
    no_cch_2179_oracle_profile_ref: enableNoCchProof ? 'claude_code_2_1_179_custom_base_no_cch_oracle_cp1_degraded_v1' : undefined,
    signed_cch_2179_oracle_profile_approved: enableSignedCchProof,
    signed_cch_2179_oracle_profile_ref: enableSignedCchProof ? 'claude_code_2_1_179_first_party_signed_cch_oracle_cp1_degraded_v1' : undefined,
  },
  account_identities: {
    [accountRef]: {
      device_id: 'b'.repeat(64),
      account_uuid_hash: 'hmac-sha256:local-harness-account-uuid',
      email_hash: 'hmac-sha256:local-harness-email',
      account_hash: accountRef,
      credential_ref: credentialRef,
      credential_binding_hmac: credentialBindingHmac(selectedCredential),
      persona_variant: 'claude-code-2.1.179-macos-local',
      session_policy: 'preserve_downstream_session_id',
      policy_version: policyVersion,
    },
  },
  egress_buckets: { 'bucket-a': { enabled: true, proxy_url: `http://127.0.0.1:${addr(proxy)}`, proxy_identity_ref: proxyIdentityRef, allowed_account_ids: [accountRef] } },
  logging: { level: 'error', audit: false },
})
const gateway = startProxy(config)
await new Promise(resolve => gateway.listening ? resolve() : gateway.once('listening', resolve))
const ccGatewayUrl = serverUrl(gateway)
const mockUrl = `http://127.0.0.1:${addr(mock)}`
function serverUrl(server) { return `http://127.0.0.1:${server.address().port}` }
function writeSummary() {
  const safe = { cc_gateway_url: ccGatewayUrl, mock_url: mockUrl, formal_pool_attested: true, persistent_ledger_configured: true, cc_gateway_root: ccGatewayRoot, profile: { policy_version: policyVersion, observed_cli_version: observedCliVersion, trusted_egress_profile_ref: trustedEgressProfileRef, billing_shape_policy: billingShapePolicy, profile_policy_version: profilePolicyVersion, request_shape_profile_ref: requestShapeProfileRef, cache_parity_profile_ref: cacheParityProfileRef, no_cch_oracle_proof_enabled: enableNoCchProof, signed_cch_oracle_proof_enabled: enableSignedCchProof }, proxy_connect_targets: proxyTargets, mock_request_count: summaries.length, mock_requests: summaries }
  writeFileSync(`${outDir}/cc_safe_summary.json`, JSON.stringify(safe, null, 2), { mode: 0o600 })
}
setInterval(writeSummary, 500).unref()
process.on('SIGTERM', () => { writeSummary(); if (previousLedgerFile === undefined) delete process.env.CC_GATEWAY_FORMAL_POOL_SESSION_LEDGER_FILE; else process.env.CC_GATEWAY_FORMAL_POOL_SESSION_LEDGER_FILE = previousLedgerFile; gateway.close(()=>{}); mock.close(()=>{}); proxy.close(()=>{}); setTimeout(()=>process.exit(0), 100) })
console.log(JSON.stringify({ cc_gateway_url: ccGatewayUrl, out_dir: outDir, mock_url: mockUrl }))
