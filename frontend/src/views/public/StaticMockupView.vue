<template>
  <main class="static-mockup-page" :aria-label="title">
    <iframe
      class="static-mockup-frame"
      :src="frameSrc"
      :title="title"
      referrerpolicy="same-origin"
    ></iframe>
  </main>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const props = defineProps<{
  src: string
  title: string
}>()

const route = useRoute()
const frameSrc = computed(() => {
  if (!route.hash) {
    return props.src
  }

  return `${props.src}${route.hash}`
})
</script>

<style scoped>
.static-mockup-page {
  min-height: 100vh;
  background: #080b16;
}

.static-mockup-frame {
  display: block;
  width: 100%;
  min-height: 100vh;
  border: 0;
}
</style>
