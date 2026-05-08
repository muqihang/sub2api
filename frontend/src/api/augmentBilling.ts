import { apiClient } from './client'

export interface AugmentBillingSummary {
  estimated_cost: number
  settled_cost: number
  free_quota: number
  paid_balance: number
  currency: string
  cache_hit_ratio: number
  total_cache_read_tokens: number
  total_cache_creation_tokens: number
}

export interface AugmentBillingUsageRow {
  model: string
  endpoint: string
  status: string
  tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  estimated_cost: number
  settled_cost: number
  pricing_version: string
  billable: boolean
  cost_source: string
  currency: string
  error_class: string
  request_id: string
  created_at?: string
}

export interface AugmentBillingRecentErrorRow {
  model: string
  endpoint: string
  status: string
  error_class: string
  request_id: string
  created_at?: string
}

export async function getAugmentBillingSummary(): Promise<AugmentBillingSummary> {
  const { data } = await apiClient.get<AugmentBillingSummary>('/plugin/augment/billing/summary')
  return data
}

export async function listAugmentBillingUsage(params: {
  page?: number
  page_size?: number
}): Promise<{
  rows: AugmentBillingUsageRow[]
  page: Record<string, unknown>
}> {
  const { data } = await apiClient.get('/plugin/augment/billing/usage', {
    params,
  })
  return data as {
    rows: AugmentBillingUsageRow[]
    page: Record<string, unknown>
  }
}

export async function listAugmentRecentErrors(params: {
  limit?: number
}): Promise<{
  rows: AugmentBillingRecentErrorRow[]
}> {
  const { data } = await apiClient.get('/plugin/augment/billing/recent-errors', {
    params,
  })
  return data as {
    rows: AugmentBillingRecentErrorRow[]
  }
}
