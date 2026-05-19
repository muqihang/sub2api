<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useCodexEntryStore } from '@/stores/codexEntry'

const { t } = useI18n()
const store = useCodexEntryStore()
</script>

<template>
  <section class="console-card model-catalog-card" data-testid="model-catalog-card">
    <div class="card-head">
      <div>
        <h3>{{ t('codex.console.modelCatalog') }}</h3>
        <p class="card-sub">{{ t('codex.console.modelCatalogHint') }}</p>
      </div>
    </div>

    <div v-if="store.modelPreview.length > 0" class="model-list">
      <article v-for="model in store.modelPreview" :key="`${model.platform}-${model.name}`" class="model-row">
        <div>
          <strong>{{ model.name }}</strong>
          <span class="vendor">{{ model.platform }}</span>
        </div>
        <span class="price">{{ model.pricing?.billing_mode ? t(`codex.console.billingModes.${model.pricing.billing_mode}`) : t('codex.console.pricingPending') }}</span>
      </article>
    </div>

    <p v-else class="empty-state placeholder-text">{{ t('codex.console.modelCatalogEmpty') }}</p>
  </section>
</template>

<style scoped>
.console-card { border: 1px solid #e5e7eb; border-radius: 12px; background: #fff; padding: 16px; }
.card-head h3 { margin: 0; }
.card-sub { margin: 6px 0 0; color: #6b7280; font-size: 13px; }
.model-list { display: grid; gap: 8px; margin-top: 12px; }
.model-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 12px; align-items: center; padding: 10px 12px; border: 1px solid #eef0f3; border-radius: 10px; }
.vendor { display: block; margin-top: 4px; color: #6b7280; font-size: 12px; }
.price { color: #4b5563; font-size: 12px; }
.empty-state { margin: 12px 0 0; padding: 12px; border-radius: 10px; background: #f9fafb; color: #6b7280; }
</style>
