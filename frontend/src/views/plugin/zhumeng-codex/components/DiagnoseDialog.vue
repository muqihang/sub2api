<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CodexDiagnoseReport } from '@/types'

const { t } = useI18n()

defineProps<{
  report: CodexDiagnoseReport
}>()

const emit = defineEmits<{
  close: []
}>()
</script>

<template>
  <div class="diagnose-dialog-overlay" data-testid="diagnose-dialog">
    <div class="diagnose-dialog">
      <div class="dialog-head">
        <div>
          <h3>{{ t('codex.console.diagnoseTitle') }}</h3>
          <p>{{ t('codex.console.diagnoseDescription') }}</p>
        </div>
        <button class="close-btn" @click="emit('close')">×</button>
      </div>
      <div class="diagnose-checks">
        <div
          v-for="check in report.checks"
          :key="check.name"
          class="diagnose-check"
          :class="`check-${check.status}`"
          :data-testid="`diagnose-check-${check.name}`"
        >
          <span class="check-status">{{ check.status }}</span>
          <div class="check-copy">
            <strong class="check-name">{{ check.name }}</strong>
            <span class="check-hint">{{ check.hint }}</span>
          </div>
        </div>
      </div>
      <div class="diagnose-summary" :class="report.ok ? 'summary-ok' : 'summary-fail'">
        {{ report.ok ? t('codex.console.diagnoseOk') : t('codex.console.diagnoseFail') }}
      </div>
      <div class="dialog-actions">
        <button class="btn btn-secondary" @click="emit('close')" data-testid="diagnose-close-btn">
          {{ t('codex.console.close') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.diagnose-dialog-overlay {
  position: fixed;
  inset: 0;
  background: rgba(17, 24, 39, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  z-index: 50;
}

.diagnose-dialog {
  width: min(680px, 100%);
  background: #fff;
  border-radius: 16px;
  padding: 20px;
  box-shadow: 0 24px 80px rgba(17, 24, 39, 0.28);
}

.dialog-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
}

.dialog-head h3 { margin: 0; }
.dialog-head p { margin: 6px 0 0; color: #6b7280; line-height: 1.65; }
.close-btn { border: 0; background: transparent; font-size: 28px; line-height: 1; cursor: pointer; color: #6b7280; }

.diagnose-checks { display: grid; gap: 10px; margin-top: 18px; }
.diagnose-check { display: flex; gap: 12px; padding: 12px; border: 1px solid #e5e7eb; border-radius: 10px; }
.diagnose-check.check-ok { background: #ecfdf5; border-color: #bbf7d0; }
.diagnose-check.check-fail { background: #fef2f2; border-color: #fecaca; }
.diagnose-check.check-warn { background: #fffbeb; border-color: #fde68a; }
.check-status { font-size: 12px; font-weight: 700; text-transform: uppercase; }
.check-copy { display: grid; gap: 4px; }
.check-name { color: #111827; }
.check-hint { color: #4b5563; font-size: 13px; line-height: 1.55; }
.diagnose-summary { margin-top: 16px; padding: 12px 14px; border-radius: 10px; font-weight: 600; }
.summary-ok { background: #ecfdf5; color: #047857; }
.summary-fail { background: #fef2f2; color: #b91c1c; }
.dialog-actions { display: flex; justify-content: flex-end; margin-top: 18px; }
.btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; padding: 10px 14px; border-radius: 10px; border: 1px solid #d1d5db; cursor: pointer; font-size: 14px; background: #fff; color: #111827; }
</style>
