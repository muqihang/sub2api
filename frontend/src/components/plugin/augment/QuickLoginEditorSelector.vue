<template>
  <section class="space-y-3">
    <p class="text-sm font-medium text-gray-900 dark:text-white">
      {{ t('plugin.augment.quickLogin.editorTargetTitle') }}
    </p>

    <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <button
        v-for="target in AUGMENT_IDE_TARGETS"
        :key="target.id"
        :data-test="`editor-target-${target.id}`"
        type="button"
        class="flex min-h-16 flex-col items-center justify-center gap-1 rounded-lg border px-4 py-3 text-center text-sm font-medium transition focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 dark:focus:ring-offset-dark-900"
        :aria-pressed="modelValue === target.id"
        :class="modelValue === target.id
          ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/20 dark:text-primary-200'
          : 'border-gray-200 bg-white text-gray-700 hover:border-primary-300 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200 dark:hover:border-primary-500/60 dark:hover:bg-dark-800'"
        @click="$emit('update:modelValue', target.id)"
      >
        <span>{{ t(target.labelKey) }}</span>
        <span class="text-xs font-normal text-gray-500 dark:text-gray-400">
          {{ t(target.statusBadgeKey) }}
        </span>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  AUGMENT_IDE_TARGETS,
  type AugmentQuickLoginEditorTarget,
} from '@/utils/augmentIdeTargets'

defineProps<{
  modelValue: AugmentQuickLoginEditorTarget
}>()

defineEmits<{
  'update:modelValue': [value: AugmentQuickLoginEditorTarget]
}>()

const { t } = useI18n()
</script>
