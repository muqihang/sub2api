import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const deployDirectory = resolve(import.meta.dirname, '..')

function environmentDefault(fileName, variableName) {
  const compose = readFileSync(resolve(deployDirectory, fileName), 'utf8')
  const match = compose.match(
    new RegExp(`- ${variableName}=\\$\\{${variableName}:-([^}]*)\\}`)
  )

  assert.ok(match, `${variableName} must define a Compose default`)
  return match[1]
}

test('development Compose keeps batch image disabled without production storage defaults', () => {
  for (const variableName of [
    'BATCH_IMAGE_ENABLED',
    'BATCH_IMAGE_QUEUE_ENABLED',
    'BATCH_IMAGE_VERTEX_ENABLED',
  ]) {
    assert.equal(environmentDefault('docker-compose.dev.yml', variableName), 'false')
  }

  for (const variableName of [
    'BATCH_IMAGE_VERTEX_PROJECT_ID',
    'BATCH_IMAGE_VERTEX_MANAGED_GCS_BUCKET',
    'BATCH_IMAGE_VERTEX_MANAGED_GCS_PREFIX',
  ]) {
    assert.equal(environmentDefault('docker-compose.dev.yml', variableName), '')
  }
})

test('production Compose disallows insecure HTTP by default', () => {
  assert.equal(
    environmentDefault('docker-compose.yml', 'SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP'),
    'false'
  )
})
