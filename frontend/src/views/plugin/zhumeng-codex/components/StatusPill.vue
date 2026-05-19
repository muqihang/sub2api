<script setup lang="ts">
defineProps<{
  status: 'ok' | 'warn' | 'fail' | 'pending'
  label: string
}>()
</script>

<template>
  <span
    class="status-pill"
    :class="`status-${status}`"
    :data-testid="`status-pill-${status}`"
  >
    <i class="dot" />
    {{ label }}
  </span>
</template>

<style scoped>
.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 5px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  border: 1px solid transparent;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: currentColor;
}

.status-ok {
  color: #047857;
  background: #ecfdf5;
  border-color: #bbf7d0;
}

.status-warn {
  color: #b45309;
  background: #fffbeb;
  border-color: #fde68a;
}

.status-fail {
  color: #b91c1c;
  background: #fef2f2;
  border-color: #fecaca;
}

.status-pending {
  color: #4338ca;
  background: #eef2ff;
  border-color: #c7d2fe;
}
</style>
