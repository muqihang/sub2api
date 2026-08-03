<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useCodexEntryStore } from '@/stores/codexEntry'

const { t } = useI18n()
const store = useCodexEntryStore()
</script>

<template>
  <section class="console-card billing-card" data-testid="billing-card">
    <div class="card-head">
      <div>
        <h3>{{ t('codex.console.billing') }}</h3>
        <p class="card-sub">{{ t('codex.console.billingHint') }}</p>
      </div>
    </div>

    <dl class="billing-kv">
      <div>
        <dt>{{ t('codex.console.balanceLabel') }}</dt>
        <dd>${{ store.currentBalance.toFixed(2) }}</dd>
      </div>
      <div>
        <dt>{{ t('codex.console.billingRuleLabel') }}</dt>
        <dd>{{ t('codex.console.billingRuleBody') }}</dd>
      </div>
      <div>
        <dt>{{ t('codex.console.reuseSourceLabel') }}</dt>
        <dd>{{ t('codex.console.reuseSourceBody') }}</dd>
      </div>
    </dl>

    <a class="billing-link" href="/purchase">{{ t('codex.console.topUpCta') }}</a>
  </section>
</template>

<style scoped>
.console-card { border: 1px solid #e5e7eb; border-radius: 12px; background: #fff; padding: 16px; }
.card-head h3 { margin: 0; }
.card-sub { margin: 6px 0 0; color: #6b7280; font-size: 13px; }
.billing-kv { display: grid; gap: 12px; margin: 14px 0 0; }
.billing-kv div { padding: 10px 12px; border: 1px solid #eef0f3; border-radius: 10px; }
.billing-kv dt { color: #6b7280; font-size: 12px; }
.billing-kv dd { margin: 6px 0 0; color: #111827; font-size: 14px; line-height: 1.6; }
.billing-link { display: inline-flex; margin-top: 14px; color: #4338ca; text-decoration: none; font-size: 13px; font-weight: 600; }
</style>
