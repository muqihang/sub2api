<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

defineProps<{
  currentStep: number
}>()

const steps = [
  { step: 1, labelKey: 'codex.wizard.steps.credential', subKey: 'codex.wizard.steps.credentialSub' },
  { step: 2, labelKey: 'codex.wizard.steps.attach', subKey: 'codex.wizard.steps.attachSub' },
  { step: 3, labelKey: 'codex.wizard.steps.verify', subKey: 'codex.wizard.steps.verifySub' },
]
</script>

<template>
  <div class="wizard-step-header" data-testid="wizard-step-header">
    <div
      v-for="s in steps"
      :key="s.step"
      class="step-indicator"
      :class="{
        active: s.step === currentStep,
        completed: s.step < currentStep,
      }"
      :data-testid="`step-indicator-${s.step}`"
    >
      <span class="step-number">{{ s.step }}</span>
      <div>
        <span class="step-label">{{ t(s.labelKey) }}</span>
        <span class="step-sub">{{ t(s.subKey) }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.wizard-step-header {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.step-indicator {
  display: flex;
  gap: 12px;
  padding: 14px;
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  background: #fff;
}

.step-indicator.active {
  border-color: #c7d2fe;
  background: #eef2ff;
}

.step-indicator.completed {
  border-color: #bbf7d0;
  background: #ecfdf5;
}

.step-number {
  width: 28px;
  height: 28px;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #111827;
  color: #fff;
  font-size: 12px;
  font-weight: 700;
  flex: 0 0 auto;
}

.step-indicator.active .step-number {
  background: #4338ca;
}

.step-indicator.completed .step-number {
  background: #047857;
}

.step-label,
.step-sub {
  display: block;
}

.step-label {
  font-weight: 700;
  color: #111827;
}

.step-sub {
  margin-top: 4px;
  color: #6b7280;
  font-size: 12px;
  line-height: 1.5;
}

@media (max-width: 860px) {
  .wizard-step-header {
    grid-template-columns: 1fr;
  }
}
</style>
