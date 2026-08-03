import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ClaudeOnboardingWizardView from '../ClaudeOnboardingWizardView.vue'

const useNewAccountManagementUx = vi.hoisted(() => ({ value: false }))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    get useNewAccountManagementUx() {
      return useNewAccountManagementUx.value
    },
  }),
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main data-testid="app-layout"><slot /></main>' },
}))

vi.mock('@/components/account/ClaudeFormalPoolOnboardingWizard.vue', () => ({
  default: { template: '<section data-testid="legacy-onboarding">legacy</section>' },
}))

vi.mock('@/components/account/ClaudeFormalPoolOnboardingWizardV2.vue', () => ({
  default: { template: '<section data-testid="v2-onboarding">v2</section>' },
}))

describe('ClaudeOnboardingWizardView flag switch', () => {
  beforeEach(() => {
    useNewAccountManagementUx.value = false
  })

  it('renders the legacy wizard when the new account management flag is false', () => {
    const wrapper = mount(ClaudeOnboardingWizardView)

    expect(wrapper.find('[data-testid="legacy-onboarding"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v2-onboarding"]').exists()).toBe(false)
  })

  it('renders the V2 wizard when the new account management flag is true', () => {
    useNewAccountManagementUx.value = true

    const wrapper = mount(ClaudeOnboardingWizardView)

    expect(wrapper.find('[data-testid="v2-onboarding"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="legacy-onboarding"]').exists()).toBe(false)
  })
})
