import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: { value: false },
    copyToClipboard: vi.fn()
  })
}))

import OAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'

describe('OAuthAuthorizationFlow Codex PAT auth', () => {
  it('renders the Codex PAT method and emits a trimmed token', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        showCookieOption: false,
        showCodexPatOption: true
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    const radio = wrapper.get('input[value="codex_pat"]')
    await radio.setValue(true)

    const textarea = wrapper.get('textarea[placeholder="admin.accounts.oauth.openai.codexPatPlaceholder"]')
    await textarea.setValue('  at-frontend-test  ')
    await wrapper.get('button.btn-primary').trigger('click')

    expect(wrapper.emitted('import-codex-pat')).toEqual([["at-frontend-test"]])
  })
})
