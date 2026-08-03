import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const viewPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AccountsView.vue')
const viewSource = readFileSync(viewPath, 'utf8')

describe('admin AccountsView scheduler score loading', () => {
  it('keeps scheduler scores hidden unless the column is visible', () => {
    expect(viewSource).toContain("'scheduler_score'")
    expect(viewSource).toContain('include_scheduler_score')
    expect(viewSource).toContain('shouldIncludeSchedulerScore')
  })

  it('migrates saved column settings to the scheduler score safe default', () => {
    expect(viewSource).toContain('account-hidden-columns-version')
    expect(viewSource).toContain('scheduler-score-hidden-by-default')
  })
})
