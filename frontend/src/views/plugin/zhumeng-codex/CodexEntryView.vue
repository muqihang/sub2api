<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCodexEntryStore } from '@/stores/codexEntry'
import AppLayout from '@/components/layout/AppLayout.vue'
import CodexWizard from './components/CodexWizard.vue'
import CodexConsole from './components/CodexConsole.vue'
import StatusPill from './components/StatusPill.vue'

const { t } = useI18n()
const store = useCodexEntryStore()
let summaryPollTimer: ReturnType<typeof setInterval> | null = null
let summaryPollInFlight = false

const shouldPollSummary = computed(() =>
  store.pageState === 'onboarding_attach' ||
  store.pageState === 'onboarding_verify' ||
  store.setupSessionPresentation === 'console_banner',
)

const heroStatus = computed(() => {
  switch (store.pageState) {
    case 'console':
      return { kind: 'ok' as const, label: t('codex.hero.connected') }
    case 'onboarding_verify':
      return { kind: 'pending' as const, label: t('codex.hero.syncing') }
    case 'onboarding_attach':
      return { kind: 'warn' as const, label: t('codex.hero.awaitingLocal') }
    default:
      return { kind: 'warn' as const, label: t('codex.hero.notConnected') }
  }
})

const heroDescription = computed(() => {
  if (store.pageState === 'console') {
    return t('codex.hero.consoleDescription', { count: store.connectedDeviceCount })
  }
  return t('codex.hero.setupDescription')
})

onMounted(() => {
  store.loadSummary()
  store.loadSupportingData()
})

onBeforeUnmount(() => {
  stopSummaryPolling()
})

watch(shouldPollSummary, (enabled) => {
  if (enabled) {
    startSummaryPolling()
  } else {
    stopSummaryPolling()
  }
}, { immediate: true })

function startSummaryPolling() {
  if (summaryPollTimer) return
  summaryPollTimer = setInterval(() => {
    void pollSummaryOnce()
  }, 2000)
}

function stopSummaryPolling() {
  if (!summaryPollTimer) return
  clearInterval(summaryPollTimer)
  summaryPollTimer = null
}

async function pollSummaryOnce() {
  if (summaryPollInFlight) return
  summaryPollInFlight = true
  try {
    await store.loadSummary({ silent: true })
  } finally {
    summaryPollInFlight = false
  }
}
</script>

<template>
  <AppLayout>
    <div class="codex-entry-view" data-testid="codex-entry-view">
    <header class="page-hero" data-testid="codex-page-hero">
      <div>
        <p class="eyebrow">{{ t('codex.hero.eyebrow') }}</p>
        <h1>{{ t('codex.hero.title') }}</h1>
        <p class="hero-description">
          {{ heroDescription }}
        </p>
      </div>
      <div class="hero-status">
        <StatusPill :status="heroStatus.kind" :label="heroStatus.label" />
      </div>
    </header>

    <div class="page-shell" data-testid="codex-page-shell">
      <div v-if="store.loading && !store.summary" class="page-state-card codex-loading">
        <p>{{ t('codex.loading') }}</p>
      </div>

      <div v-else-if="store.error && !store.summary" class="page-state-card codex-error">
        <p>{{ store.error }}</p>
        <button class="btn btn-primary" @click="store.loadSummary()">{{ t('codex.retry') }}</button>
      </div>

      <template v-else>
        <CodexWizard
          v-if="store.pageState !== 'console'"
          data-testid="codex-wizard"
        />

        <CodexConsole
          v-else
          data-testid="codex-console"
        />
      </template>
    </div>
    </div>
  </AppLayout>
</template>

<style scoped>
.codex-entry-view {
  --bg: #f7f7f8;
  --ink: #111827;
  --ink-2: #4b5563;
  --ink-3: #6b7280;
  --line: #e5e7eb;
  --line-soft: #eef0f3;
  --primary: #111827;
  --primary-ink: #ffffff;
  --accent: #4338ca;
  --accent-soft: #eef2ff;
  --accent-line: #c7d2fe;
  --ok: #047857;
  --ok-soft: #ecfdf5;
  --warn: #b45309;
  --warn-soft: #fffbeb;
  --danger: #b91c1c;
  --danger-soft: #fef2f2;
  --muted-bg: #f9fafb;
  min-height: 100%;
  background: var(--bg);
  color: var(--ink);
  padding: 32px 24px 80px;
}

.page-hero,
.page-shell {
  max-width: 1200px;
  margin: 0 auto;
}

.page-hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 20px;
  align-items: end;
  margin-bottom: 22px;
}

.eyebrow {
  margin: 0 0 8px;
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ink-3);
}

.page-hero h1 {
  margin: 0;
  font-size: 28px;
  line-height: 1.2;
}

.hero-description {
  margin: 10px 0 0;
  max-width: 700px;
  color: var(--ink-2);
  font-size: 14px;
  line-height: 1.75;
}

.hero-status {
  display: flex;
  justify-content: flex-end;
}

.page-shell {
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 16px;
  box-shadow: 0 18px 60px rgba(17, 24, 39, 0.06);
  overflow: hidden;
}

.page-state-card {
  padding: 48px 32px;
  text-align: center;
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 16px;
  border-radius: 10px;
  border: 1px solid transparent;
  cursor: pointer;
  font-size: 14px;
}

.btn-primary {
  background: var(--primary);
  color: var(--primary-ink);
}

.codex-loading,
.codex-error {
  color: var(--ink-2);
}

@media (max-width: 900px) {
  .codex-entry-view {
    padding: 20px 16px 48px;
  }

  .page-hero {
    grid-template-columns: 1fr;
  }

  .hero-status {
    justify-content: flex-start;
  }
}
</style>
