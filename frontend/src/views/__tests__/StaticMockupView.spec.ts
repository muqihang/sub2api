import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import StaticMockupView from '@/views/public/StaticMockupView.vue'

const routeHash = ref('')

vi.mock('vue-router', () => ({
  useRoute: () => ({
    get hash() {
      return routeHash.value
    },
  }),
}))

describe('StaticMockupView', () => {
  it('renders a full-page iframe for a static marketing mockup', () => {
    routeHash.value = ''

    const wrapper = mount(StaticMockupView, {
      props: {
        src: '/brand/mockups/homepage-codex-premium-v6.html',
        title: '逐梦 Agent 首页',
      },
    })

    const iframe = wrapper.get('iframe')
    expect(iframe.attributes('src')).toBe('/brand/mockups/homepage-codex-premium-v6.html')
    expect(iframe.attributes('title')).toBe('逐梦 Agent 首页')
    expect(iframe.classes()).toContain('static-mockup-frame')
  })

  it('forwards the current route hash into the static mockup iframe', () => {
    routeHash.value = '#products'

    const wrapper = mount(StaticMockupView, {
      props: {
        src: '/brand/mockups/homepage-codex-premium-v6.html',
        title: '逐梦 Agent 首页',
      },
    })

    expect(wrapper.get('iframe').attributes('src')).toBe(
      '/brand/mockups/homepage-codex-premium-v6.html#products'
    )
  })
})
