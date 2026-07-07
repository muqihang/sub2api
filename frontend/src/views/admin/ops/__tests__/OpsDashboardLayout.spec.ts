import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const sourcePath = resolve(process.cwd(), 'src/views/admin/ops/OpsDashboard.vue')

describe('OpsDashboard layout', () => {
  it('bounds responsive trend chart cards to a fixed height', () => {
    const source = readFileSync(sourcePath, 'utf8')

    expect(source).toMatch(/<div class="lg:col-span-1 min-h-\[360px\]">\s*<OpsConcurrencyCard/)
    expect(source).toMatch(/<div class="lg:col-span-1 h-\[360px\]">\s*<OpsSwitchRateTrendChart/)
    expect(source).toMatch(/<div class="lg:col-span-2 h-\[360px\]">\s*<OpsThroughputTrendChart/)
  })
})
