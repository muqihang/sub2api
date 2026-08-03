import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ZhumengAgentSetupPanel from '../ZhumengAgentSetupPanel.vue'

const mockCreateCodexSetupGrant = vi.fn()

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/api/zhumengAgent', () => ({
  createCodexSetupGrant: (...args: any[]) => mockCreateCodexSetupGrant(...args),
}))

vi.mock('vue-router', () => ({
  RouterLink: {
    template: '<a data-testid="go-to-codex-entry-link"><slot /></a>',
    props: ['to'],
  },
  useRouter: () => ({ push: vi.fn() }),
}))

describe('ZhumengAgentSetupPanel (slimmed)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('does NOT have a "create independent credential" button', () => {
    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: 42 },
    })
    // The panel should only have quick-setup (reuse current key), not independent creation.
    expect(wrapper.find('[data-testid="quick-setup-btn"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('independent')
    expect(wrapper.text()).not.toContain('创建独立凭证')
  })

  it('does NOT have repair or diagnose controls', () => {
    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: 42 },
    })
    expect(wrapper.find('[data-testid="repair-btn"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="diagnose-btn"]').exists()).toBe(false)
  })

  it('has a clear link to the dedicated Codex entry page', () => {
    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: 42 },
    })
    expect(wrapper.find('[data-testid="go-to-codex-entry-link"]').exists()).toBe(true)
  })

  it('disables quick-setup when apiKeyId is null', () => {
    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: null },
    })
    const btn = wrapper.find('[data-testid="quick-setup-btn"]')
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('calls createCodexSetupGrant with the current key (reuse path only)', async () => {
    mockCreateCodexSetupGrant.mockResolvedValue({ code: 'abc', expires_at: '2026-01-01', deeplink: 'zhumeng-agent://setup?code=abc' })

    const wrapper = mount(ZhumengAgentSetupPanel, {
      props: { apiKeyId: 42 },
    })
    await wrapper.find('[data-testid="quick-setup-btn"]').trigger('click')
    // Wait for async
    await vi.dynamicImportSettled()

    expect(mockCreateCodexSetupGrant).toHaveBeenCalledWith(42)
  })
})
