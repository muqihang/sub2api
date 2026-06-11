import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const {
  createCodexSetupGrant,
  listCodexManagedDevices,
  revokeCodexManagedDevice,
} = vi.hoisted(() => ({
  createCodexSetupGrant: vi.fn(),
  listCodexManagedDevices: vi.fn(),
  revokeCodexManagedDevice: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn().mockResolvedValue(true)
  })
}))

vi.mock('@/api/zhumengAgent', () => ({
  createCodexSetupGrant,
  listCodexManagedDevices,
  revokeCodexManagedDevice,
}))

import UseKeyModal from '../UseKeyModal.vue'

describe('UseKeyModal', () => {
  it('renders GPT-5.5, Codex-safe context limits, and goals feature in OpenAI Codex config', () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKeyId: 42,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('model_provider = "OpenAI"'))

    expect(configToml).toBeDefined()
    expect(configToml).toContain('model = "gpt-5.5"')
    expect(configToml).toContain('review_model = "gpt-5.5"')
    expect(configToml).toContain('model_context_window = 272000')
    expect(configToml).toContain('model_auto_compact_token_limit = 244800')
    expect(configToml).toContain('[features]\ngoals = true')
  })

  it('renders GPT-5.5, Codex-safe context limits, and goals feature in OpenAI Codex WebSocket config', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKeyId: 42,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const wsTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCliWs')
    )

    expect(wsTab).toBeDefined()
    await wsTab!.trigger('click')
    await nextTick()

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('supports_websockets = true'))

    expect(configToml).toBeDefined()
    expect(configToml).toContain('model = "gpt-5.5"')
    expect(configToml).toContain('review_model = "gpt-5.5"')
    expect(configToml).toContain('model_context_window = 272000')
    expect(configToml).toContain('model_auto_compact_token_limit = 244800')
    expect(configToml).toContain('[features]\nresponses_websockets_v2 = true\ngoals = true')
  })

  it('allows switching the generated OpenAI config between GPT-5.4 and GPT-5.5', async () => {
    listCodexManagedDevices.mockResolvedValue([])
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKeyId: 42,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    expect(wrapper.text()).toContain('gpt-5.4')
    expect(wrapper.text()).toContain('gpt-5.5')
    expect(wrapper.find('pre code').text()).toContain('model = "gpt-5.5"')

    const model54Button = wrapper.findAll('button').find((button) => button.text().includes('gpt-5.4'))
    expect(model54Button).toBeDefined()
    await model54Button!.trigger('click')
    await nextTick()

    const code = wrapper.find('pre code').text()
    expect(code).toContain('model = "gpt-5.4"')
    expect(code).toContain('review_model = "gpt-5.4"')
  })

  it('uses GPT-5.5 Codex-safe context limits in generated configs', async () => {
    listCodexManagedDevices.mockResolvedValue([])
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKeyId: 42,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const code = wrapper.find('pre code').text()
    expect(code).toContain('model = "gpt-5.5"')
    expect(code).toContain('model_context_window = 272000')
    expect(code).toContain('model_auto_compact_token_limit = 244800')

    const model54Button = wrapper.findAll('button').find((button) => button.text().includes('gpt-5.4'))
    expect(model54Button).toBeDefined()
    await model54Button!.trigger('click')
    await nextTick()

    const gpt54Code = wrapper.find('pre code').text()
    expect(gpt54Code).toContain('model = "gpt-5.4"')
    expect(gpt54Code).toContain('model_context_window = 1050000')
    expect(gpt54Code).toContain('model_auto_compact_token_limit = 900000')
  })

  it('renders updated GPT-5.4 mini/nano names in OpenCode config', async () => {
    listCodexManagedDevices.mockResolvedValue([])
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKeyId: 42,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )

    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    expect(codeBlock.text()).toContain('"name": "GPT-5.4 Mini"')
    expect(codeBlock.text()).not.toContain('"name": "GPT-5.4 Nano"')
  })

  it('renders Claude Fable 5 adaptive thinking in Antigravity OpenCode config', async () => {
    listCodexManagedDevices.mockResolvedValue([])
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKeyId: 42,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'antigravity-claude'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    const config = codeBlock.text()
    const parsed = JSON.parse(config)
    const fable = parsed.provider['antigravity-claude'].models['claude-fable-5']
    expect(fable.limit.context).toBe(1048576)
    expect(fable.limit.output).toBe(128000)
    expect(fable.options.thinking).toEqual({ type: 'adaptive' })
  })

  it('shows augment-only guidance instead of generic client snippets', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKeyId: 42,
        apiKey: 'sk-augment',
        baseUrl: 'https://example.com/v1',
        platform: 'openai',
        augmentOnly: true,
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    expect(wrapper.text()).toContain('keys.useKeyModal.augmentOnly.title')
    expect(wrapper.text()).toContain('keys.useKeyModal.augmentOnly.description')
    expect(wrapper.text()).toContain('keys.useKeyModal.augmentOnly.quickLoginNote')
    expect(wrapper.find('pre code').exists()).toBe(false)
  })

  it('passes apiKeyId to zhumeng-agent setup and does not leak raw api key', async () => {
    vi.useFakeTimers()
    vi.spyOn(window, 'open').mockImplementation(() => null)
    listCodexManagedDevices.mockResolvedValue([])
    createCodexSetupGrant.mockResolvedValue({
      code: 'grant-1',
      expires_at: '2026-05-11T12:00:00Z',
      deeplink: 'zhumeng-agent://setup?client=codex&code=grant-1',
    })

    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKeyId: 42,
        apiKey: 'sk-secret',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const setupButton = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.zhumengAgent.quickSetup')
    )
    expect(setupButton).toBeDefined()
    await setupButton!.trigger('click')

    expect(createCodexSetupGrant).toHaveBeenCalledWith(42)
    expect(window.open).toHaveBeenCalledWith('zhumeng-agent://setup?client=codex&code=grant-1', '_self')
    expect(JSON.stringify(createCodexSetupGrant.mock.calls)).not.toContain('sk-secret')
    expect(JSON.stringify(window.open.mock.calls)).not.toContain('sk-secret')
    await vi.advanceTimersByTimeAsync(1500)
    // Slimmed panel no longer shows fallback help; verify link to entry page instead
    expect(wrapper.text()).toContain('keys.zhumengAgent.goToEntryPage')
  })
})
