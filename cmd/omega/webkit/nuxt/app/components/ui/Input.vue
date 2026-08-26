<script setup lang="ts">
defineProps<{ invalid?: boolean }>()
const model = defineModel<string | number>()
</script>

<template>
  <input
    v-model="model"
    :aria-invalid="invalid || undefined"
    class="h-8 w-full min-w-0 rounded-md border border-outline-gray-2 bg-surface-base px-2.5 py-1 text-base text-ink-gray-9 shadow-2xs outline-none transition-[color,border-color,box-shadow] duration-100 placeholder:text-ink-gray-4 hover:border-outline-gray-3 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/35 disabled:cursor-not-allowed disabled:bg-surface-gray-2 disabled:text-ink-gray-4 aria-invalid:border-destructive aria-invalid:ring-2 aria-invalid:ring-destructive/25"
  >
</template>
