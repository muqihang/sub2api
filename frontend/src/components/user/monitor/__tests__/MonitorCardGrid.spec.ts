import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import MonitorCardGrid from '../MonitorCardGrid.vue'
import type { UserMonitorView } from '@/api/channelMonitor'

const monitor = (id: number, name: string, groupName: string): UserMonitorView => ({
  id,
  name,
  provider: 'openai',
  group_name: groupName,
  primary_model: name,
  primary_status: 'operational',
  primary_latency_ms: 100,
  primary_ping_latency_ms: 10,
  availability_7d: 100,
  extra_models: [],
  timeline: [],
})

describe('MonitorCardGrid', () => {
  it('renders monitors with the same group name under one status group', () => {
    const wrapper = mount(MonitorCardGrid, {
      props: {
        items: [
          monitor(5, 'Zhumeng-embeddings-1536', '逐梦 向量/排序分组'),
          monitor(6, 'Zhumeng-erank', '逐梦 向量/排序分组'),
        ],
        window: '7d',
        countdownSeconds: 30,
        loading: false,
        detailCache: {},
      },
      global: {
        stubs: {
          MonitorCard: {
            props: ['item'],
            template: '<article data-monitor-card>{{ item.name }}</article>',
          },
          EmptyState: { template: '<div />' },
        },
      },
    })

    expect(wrapper.findAll('[data-monitor-group]')).toHaveLength(1)
    expect(wrapper.find('[data-monitor-group]').text()).toContain('逐梦 向量/排序分组')
    expect(wrapper.findAll('[data-monitor-card]')).toHaveLength(2)
  })
})
