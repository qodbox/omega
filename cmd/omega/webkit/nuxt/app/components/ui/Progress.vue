<script setup lang="ts">
const props = defineProps<{ value: number }>()
const clamped = computed(() => Math.min(100, Math.max(0, props.value)))
</script>

<template>
  <div
    class="relative h-1.5 w-full overflow-hidden rounded-full bg-surface-gray-3"
    role="progressbar"
    :aria-valuenow="clamped"
    aria-valuemin="0"
    aria-valuemax="100"
  >
    <div class="h-full bg-brand transition-all" :style="{ width: `${clamped}%` }" />
  </div>
</template>
