import { describe, expect, it } from 'vitest'
import { claudeModels, getModelsByPlatform, getPresetMappingsByPlatform, buildModelMappingObject } from './useModelWhitelist'

describe('useModelWhitelist Opus 4.8 support', () => {
  it('exposes claude-opus-4-8 for Anthropic and Antigravity without downgrading mappings', () => {
    expect(claudeModels).toContain('claude-opus-4-8')
    expect(getModelsByPlatform('antigravity')).toContain('claude-opus-4-8')

    const anthropicPreset = getPresetMappingsByPlatform('anthropic').find((m) => m.from === 'claude-opus-4-8')
    expect(anthropicPreset?.to).toBe('claude-opus-4-8')

    const antigravityPreset = getPresetMappingsByPlatform('antigravity').find((m) => m.from === 'claude-opus-4-8')
    expect(antigravityPreset?.to).toBe('claude-opus-4-8')
  })

  it('builds whitelist mappings for claude-opus-4-8 as direct pass-through', () => {
    expect(buildModelMappingObject('whitelist', ['claude-opus-4-8'], [])).toEqual({
      'claude-opus-4-8': 'claude-opus-4-8',
    })
  })
})

describe('useModelWhitelist Fable 5 support', () => {
  it('exposes claude-fable-5 for Anthropic and Antigravity as direct pass-through', () => {
    expect(claudeModels).toContain('claude-fable-5')
    expect(getModelsByPlatform('antigravity')).toContain('claude-fable-5')

    const anthropicPreset = getPresetMappingsByPlatform('anthropic').find((m) => m.from === 'claude-fable-5')
    expect(anthropicPreset?.to).toBe('claude-fable-5')

    const antigravityPreset = getPresetMappingsByPlatform('antigravity').find((m) => m.from === 'claude-fable-5')
    expect(antigravityPreset?.to).toBe('claude-fable-5')

    const bedrockPreset = getPresetMappingsByPlatform('bedrock').find((m) => m.from === 'claude-fable-5')
    expect(bedrockPreset?.to).toBe('anthropic.claude-fable-5')
  })
})
