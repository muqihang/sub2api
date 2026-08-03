import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

const { getAvailableModels } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { getAvailableModels },
  },
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const SelectStub = defineComponent({
  props: {
    modelValue: { type: [String, Number], default: '' },
    options: { type: Array, default: () => [] },
    valueKey: { type: String, default: 'value' },
    labelKey: { type: String, default: 'label' },
  },
  emits: ['update:modelValue'],
  template: `<select :value="modelValue" @change="$emit('update:modelValue', $event.target.value)"><option v-for="option in options" :key="option[valueKey]" :value="option[valueKey]">{{ option[labelKey] }}</option></select>`,
})

const TextAreaStub = defineComponent({
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  template: `<textarea :value="modelValue" @input="$emit('update:modelValue', $event.target.value)" />`,
})

function account() {
  return {
    id: 88,
    name: 'OpenAI OAuth',
    platform: 'openai',
    type: 'oauth',
    status: 'active',
    credentials: {},
    extra: {},
  } as any
}

describe('admin AccountTestModal API base', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com/api/v1')
    getAvailableModels.mockReset()
    getAvailableModels.mockResolvedValue([{ id: 'gpt-5.4', display_name: 'GPT-5.4' }])
    localStorage.setItem('auth_token', 'test-token')
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: { getReader: () => ({ read: vi.fn().mockResolvedValue({ done: true, value: undefined }) }) },
    } as any)
  })

  afterEach(() => {
    localStorage.clear()
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
  })

  it('posts the streaming account test to the configured API base', async () => {
    const { default: AccountTestModal } = await import('../AccountTestModal.vue')
    const wrapper = mount(AccountTestModal, {
      props: { show: true, account: account() },
      global: { stubs: { BaseDialog: BaseDialogStub, Select: SelectStub, TextArea: TextAreaStub, Icon: true } },
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect((global.fetch as any).mock.calls[0][0]).toBe(
      'https://api.example.com/api/v1/admin/accounts/88/test'
    )
  })
})
