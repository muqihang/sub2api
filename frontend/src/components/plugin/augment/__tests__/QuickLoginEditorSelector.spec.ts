import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import QuickLoginEditorSelector from '@/components/plugin/augment/QuickLoginEditorSelector.vue'
import { AUGMENT_IDE_TARGETS } from '@/utils/augmentIdeTargets'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

describe('QuickLoginEditorSelector', () => {
  it('renders all supported IDE targets with their configured badges', () => {
    const wrapper = mount(QuickLoginEditorSelector, {
      props: {
        modelValue: 'vscode',
      },
    })

    const cards = wrapper.findAll('[data-test^="editor-target-"]')
    expect(cards).toHaveLength(8)

    for (const target of AUGMENT_IDE_TARGETS) {
      expect(wrapper.get(`[data-test="editor-target-${target.id}"]`).text()).toContain(target.labelKey)
      expect(wrapper.get(`[data-test="editor-target-${target.id}"]`).text()).toContain(target.statusBadgeKey)
    }
  })

  it('marks the selected card and emits updates when a different IDE is chosen', async () => {
    const wrapper = mount(QuickLoginEditorSelector, {
      props: {
        modelValue: 'vscode',
      },
    })

    expect(wrapper.get('[data-test="editor-target-vscode"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-test="editor-target-cursor"]').attributes('aria-pressed')).toBe('false')

    await wrapper.get('[data-test="editor-target-cursor"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([['cursor']])
  })
})
