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
    expect(codeBlock.text()).toContain('"name": "GPT-5.4 Nano"')
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
      button.text().includes('keys.zhumengAgent.setup')
    )
    expect(setupButton).toBeDefined()
    await setupButton!.trigger('click')

    expect(createCodexSetupGrant).toHaveBeenCalledWith(42)
    expect(window.open).toHaveBeenCalledWith('zhumeng-agent://setup?client=codex&code=grant-1', '_self')
    expect(JSON.stringify(createCodexSetupGrant.mock.calls)).not.toContain('sk-secret')
    expect(JSON.stringify(window.open.mock.calls)).not.toContain('sk-secret')
    await vi.advanceTimersByTimeAsync(1500)
    expect(wrapper.text()).toContain('keys.zhumengAgent.fallbackHelp')
  })
})
