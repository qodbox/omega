<script setup lang="ts">
const props = defineProps<{ name?: string, src?: string }>()

const initials = computed(() =>
  (props.name ?? '?')
    .split(' ')
    .map(part => part[0] ?? '')
    .slice(0, 2)
    .join('')
    .toUpperCase(),
)
</script>

<template>
  <span class="relative flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-full bg-surface-gray-2 text-sm text-ink-gray-6 select-none">
    <img v-if="src" :src="src" :alt="name" class="size-full object-cover">
    <template v-else>{{ initials }}</template>
  </span>
</template>
