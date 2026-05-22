<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatScaled } from '@/utils/pricing'
import { useCodexEntryStore, type CodexModelPreview } from '@/stores/codexEntry'
import type { CodexEntryModelPricing, CodexEntryPricingInterval } from '@/types'

const { t } = useI18n()
const store = useCodexEntryStore()

type PreviewPricing = Omit<CodexEntryModelPricing, 'source'> & { source?: string }

const tooltipModel = ref<CodexModelPreview | null>(null)
const tooltipStyle = ref<Record<string, string>>({ top: '0px', left: '0px' })

function modelDisplayName(model: CodexModelPreview): string {
  return 'display_name' in model && model.display_name ? model.display_name : model.name
}

function modelPricing(model: CodexModelPreview): PreviewPricing | null {
  if (!('pricing' in model) || !model.pricing) return null
  return model.pricing as PreviewPricing
}

function pricingLabel(model: CodexModelPreview): string {
  const pricing = modelPricing(model)
  if (!pricing?.billing_mode) return t('codex.console.pricingPending')
  return t(`codex.console.billingModes.${pricing.billing_mode}`)
}

function pricingSourceLabel(pricing: PreviewPricing): string {
  if (!pricing.source) return ''
  return t(`codex.console.pricingSourceLabels.${pricing.source}`)
}

function formatTokenPrice(value: number | null | undefined): string {
  return `${formatScaled(value ?? null, 1_000_000)} ${t('usage.perMillionTokens')}`
}

function formatRequestPrice(value: number | null | undefined): string {
  return `${formatScaled(value ?? null, 1)} ${t('codex.console.unitPerRequest')}`
}

function formatRange(interval: CodexEntryPricingInterval): string {
  const max = interval.max_tokens == null ? '∞' : String(interval.max_tokens)
  return interval.tier_label || `(${interval.min_tokens}, ${max}]`
}

function formatIntervalPrice(interval: CodexEntryPricingInterval, mode: string): string {
  if (mode === 'per_request' || mode === 'image') {
    return formatRequestPrice(interval.per_request_price)
  }
  return `${formatTokenPrice(interval.input_price)} / ${formatTokenPrice(interval.output_price)}`
}

async function showPricing(event: MouseEvent | FocusEvent, model: CodexModelPreview) {
  if (!modelPricing(model)) return
  const target = event.currentTarget as HTMLElement | null
  if (!target) return
  const rect = target.getBoundingClientRect()
  const margin = 10
  tooltipStyle.value = {
    top: `${rect.bottom + margin}px`,
    left: `${Math.min(Math.max(8, rect.right - 320), window.innerWidth - 328)}px`,
  }
  tooltipModel.value = model
  await nextTick()
}

function hidePricing() {
  tooltipModel.value = null
}

onBeforeUnmount(hidePricing)
</script>

<template>
  <section class="console-card model-catalog-card" data-testid="model-catalog-card">
    <div class="card-head">
      <div>
        <h3>{{ t('codex.console.modelCatalog') }}</h3>
        <p class="card-sub">{{ t('codex.console.modelCatalogHint') }}</p>
      </div>
    </div>

    <div
      v-if="store.modelPreview.length > 0"
      class="model-list model-list--scrollable"
      data-testid="model-list"
    >
      <article v-for="model in store.modelPreview" :key="`${model.platform}-${model.name}`" class="model-row">
        <div>
          <strong>{{ modelDisplayName(model) }}</strong>
          <span class="vendor">{{ model.platform }}</span>
        </div>
        <button
          type="button"
          class="price price-trigger"
          :data-testid="`model-pricing-trigger-${model.name}`"
          @mouseenter="showPricing($event, model)"
          @mouseleave="hidePricing"
          @focus="showPricing($event, model)"
          @blur="hidePricing"
        >
          {{ pricingLabel(model) }}
        </button>
      </article>
    </div>

    <p v-else class="empty-state placeholder-text">{{ t('codex.console.modelCatalogEmpty') }}</p>

    <Teleport to="body">
      <div
        v-if="tooltipModel && modelPricing(tooltipModel)"
        class="pricing-tooltip"
        role="tooltip"
        :style="tooltipStyle"
      >
        <div class="tooltip-head">
          <strong>{{ modelDisplayName(tooltipModel) }}</strong>
          <span>{{ tooltipModel.platform }}</span>
        </div>
        <div class="tooltip-body">
          <div class="tooltip-row">
            <span>{{ t('codex.console.pricingBillingMode') }}</span>
            <strong>{{ pricingLabel(tooltipModel) }}</strong>
          </div>
          <div v-if="pricingSourceLabel(modelPricing(tooltipModel)!)" class="tooltip-row">
            <span>{{ t('codex.console.pricingSource') }}</span>
            <strong>{{ pricingSourceLabel(modelPricing(tooltipModel)!) }}</strong>
          </div>

          <template v-if="modelPricing(tooltipModel)!.billing_mode === 'token'">
            <div class="tooltip-row">
              <span>{{ t('usage.inputTokenPrice') }}</span>
              <strong class="price-input">{{ formatTokenPrice(modelPricing(tooltipModel)!.input_price) }}</strong>
            </div>
            <div class="tooltip-row">
              <span>{{ t('usage.outputTokenPrice') }}</span>
              <strong class="price-output">{{ formatTokenPrice(modelPricing(tooltipModel)!.output_price) }}</strong>
            </div>
            <div class="tooltip-row">
              <span>{{ t('codex.console.cacheWritePrice') }}</span>
              <strong>{{ formatTokenPrice(modelPricing(tooltipModel)!.cache_write_price) }}</strong>
            </div>
            <div class="tooltip-row">
              <span>{{ t('codex.console.cacheReadPrice') }}</span>
              <strong>{{ formatTokenPrice(modelPricing(tooltipModel)!.cache_read_price) }}</strong>
            </div>
          </template>

          <template v-else>
            <div class="tooltip-row">
              <span>{{ t('codex.console.perRequestPrice') }}</span>
              <strong>{{ formatRequestPrice(modelPricing(tooltipModel)!.per_request_price ?? modelPricing(tooltipModel)!.image_output_price) }}</strong>
            </div>
          </template>

          <div v-if="modelPricing(tooltipModel)!.intervals?.length" class="tooltip-intervals">
            <div class="tooltip-section-title">{{ t('codex.console.pricingIntervals') }}</div>
            <div
              v-for="(interval, idx) in modelPricing(tooltipModel)!.intervals"
              :key="idx"
              class="tooltip-row tooltip-row--compact"
            >
              <span>{{ formatRange(interval) }}</span>
              <strong>{{ formatIntervalPrice(interval, modelPricing(tooltipModel)!.billing_mode) }}</strong>
            </div>
          </div>
        </div>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.console-card { border: 1px solid #e5e7eb; border-radius: 8px; background: #fff; padding: 16px; }
.card-head h3 { margin: 0; }
.card-sub { margin: 6px 0 0; color: #6b7280; font-size: 13px; }
.model-list { display: grid; gap: 8px; margin-top: 12px; padding-right: 4px; }
.model-list--scrollable { max-height: 360px; overflow-y: auto; overscroll-behavior: contain; }
.model-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 12px; align-items: center; padding: 10px 12px; border: 1px solid #eef0f3; border-radius: 8px; }
.vendor { display: block; margin-top: 4px; color: #6b7280; font-size: 12px; }
.price { color: #4b5563; font-size: 12px; }
.price-trigger { border: 1px solid #d1d5db; border-radius: 999px; background: #f9fafb; padding: 5px 9px; cursor: help; white-space: nowrap; }
.price-trigger:hover,
.price-trigger:focus-visible { border-color: #9ca3af; background: #f3f4f6; outline: none; color: #111827; }
.empty-state { margin: 12px 0 0; padding: 12px; border-radius: 8px; background: #f9fafb; color: #6b7280; }

.pricing-tooltip {
  position: fixed;
  z-index: 99999;
  width: 320px;
  max-width: calc(100vw - 16px);
  border: 1px solid rgba(55, 65, 81, 0.9);
  border-radius: 8px;
  background: #111827;
  color: #f9fafb;
  box-shadow: 0 16px 40px rgba(15, 23, 42, 0.28);
  pointer-events: none;
  font-size: 12px;
}
.tooltip-head { display: flex; justify-content: space-between; gap: 12px; padding: 10px 12px; border-bottom: 1px solid rgba(156, 163, 175, 0.24); }
.tooltip-head strong { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tooltip-head span { color: #9ca3af; text-transform: uppercase; }
.tooltip-body { display: grid; gap: 7px; padding: 10px 12px; }
.tooltip-row { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.tooltip-row span { color: #9ca3af; }
.tooltip-row strong { font-weight: 600; text-align: right; }
.tooltip-row--compact { font-size: 11px; }
.price-input { color: #7dd3fc; }
.price-output { color: #c4b5fd; }
.tooltip-intervals { margin-top: 4px; border-top: 1px solid rgba(156, 163, 175, 0.24); padding-top: 7px; display: grid; gap: 6px; }
.tooltip-section-title { color: #d1d5db; font-weight: 600; }
</style>
