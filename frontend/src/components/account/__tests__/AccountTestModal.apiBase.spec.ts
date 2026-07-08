import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

const { getAvailableModels, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  copyToClipboard: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { getAvailableModels },
  },
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

describe('AccountTestModal API base', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com/api/v1')
    getAvailableModels.mockReset()
    getAvailableModels.mockResolvedValue([{ id: 'gpt-5.4', display_name: 'GPT-5.4' }])
    copyToClipboard.mockReset()
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn(),
      },
      configurable: true,
    })
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: { getReader: () => ({ read: vi.fn().mockResolvedValue({ done: true, value: undefined }) }) },
    } as any)
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
  })

  it('posts the user account test to the configured API base', async () => {
    const { default: AccountTestModal } = await import('../AccountTestModal.vue')
    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: {
          id: 89,
          name: 'OpenAI OAuth',
          platform: 'openai',
          type: 'oauth',
          status: 'active',
          credentials: {},
          extra: {},
        } as any,
      },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          Select: defineComponent({
            props: ['modelValue', 'options', 'valueKey', 'labelKey'],
            emits: ['update:modelValue'],
            template: `<select :value="modelValue" @change="$emit('update:modelValue', $event.target.value)"><option v-for="option in options" :key="option[valueKey || 'value']" :value="option[valueKey || 'value']">{{ option[labelKey || 'label'] }}</option></select>`,
          }),
          TextArea: defineComponent({
            props: ['modelValue'],
            emits: ['update:modelValue'],
            template: `<textarea :value="modelValue" @input="$emit('update:modelValue', $event.target.value)" />`,
          }),
          Icon: true,
        },
      },
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect((global.fetch as any).mock.calls[0][0]).toBe(
      'https://api.example.com/api/v1/admin/accounts/89/test'
    )
  })
})
