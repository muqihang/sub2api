<template>
  <section class="space-y-3">
    <div class="space-y-1">
      <p class="text-sm font-medium text-gray-900 dark:text-white">
        {{ t('plugin.augment.quickLogin.editorTargetTitle') }}
      </p>
      <p class="text-sm text-gray-600 dark:text-gray-300">
        {{ t('plugin.augment.quickLogin.editorTargetDescription') }}
      </p>
    </div>

    <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <button
        v-for="target in AUGMENT_IDE_TARGETS"
        :key="target.id"
        :data-test="`editor-target-${target.id}`"
        type="button"
        class="flex min-h-32 flex-col gap-3 rounded-lg border p-4 text-left transition focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 dark:focus:ring-offset-dark-900"
        :aria-pressed="modelValue === target.id"
        :class="modelValue === target.id
          ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/20 dark:text-primary-200'
          : 'border-gray-200 bg-white text-gray-700 hover:border-primary-300 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200 dark:hover:border-primary-500/60 dark:hover:bg-dark-800'"
        @click="$emit('update:modelValue', target.id)"
      >
        <div class="flex items-start justify-between gap-3">
          <span
            class="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border text-xs font-semibold"
            :class="modelValue === target.id
              ? 'border-primary-200 bg-white/80 text-primary-700 dark:border-primary-500/40 dark:bg-primary-950/60 dark:text-primary-100'
              : 'border-gray-200 bg-gray-50 text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100'"
          >
            {{ target.shortLabel }}
          </span>
          <span
            class="inline-flex rounded-full px-2 py-1 text-[11px] font-medium"
            :class="badgeClass(target.status)"
          >
            {{ t(target.statusBadgeKey) }}
          </span>
        </div>

        <div class="space-y-1">
          <p class="text-sm font-semibold">
            {{ t(target.labelKey) }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t(target.descriptionKey) }}
          </p>
        </div>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  AUGMENT_IDE_TARGETS,
  type AugmentQuickLoginEditorTarget,
  type AugmentQuickLoginEditorTargetStatus,
} from '@/utils/augmentIdeTargets'

defineProps<{
  modelValue: AugmentQuickLoginEditorTarget
}>()

defineEmits<{
  'update:modelValue': [value: AugmentQuickLoginEditorTarget]
}>()

const { t } = useI18n()

function badgeClass(status: AugmentQuickLoginEditorTargetStatus): string {
  switch (status) {
    case 'verified':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-200'
    case 'requiresOverride':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-200'
    default:
      return 'bg-slate-100 text-slate-700 dark:bg-slate-900/40 dark:text-slate-200'
  }
}
</script>
