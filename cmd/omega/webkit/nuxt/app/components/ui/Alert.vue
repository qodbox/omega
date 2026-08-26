<script setup lang="ts">
const props = withDefaults(defineProps<{
  variant?: 'default' | 'success' | 'warning' | 'info' | 'destructive'
  title?: string
}>(), { variant: 'default' })

const variants = {
  default: 'border-outline-gray-1 bg-card text-ink-gray-8',
  success: 'border-outline-green bg-surface-green-1 text-ink-green',
  warning: 'border-outline-amber bg-surface-amber-1 text-ink-amber',
  info: 'border-outline-blue bg-surface-blue-1 text-ink-blue',
  destructive: 'border-outline-red bg-surface-red-1 text-ink-red',
}
</script>

<template>
  <div
    role="alert"
    class="grid w-full gap-0.5 rounded-md border px-3 py-2.5 text-left text-base"
    :class="variants[props.variant]"
  >
    <p v-if="title" class="font-medium">
      {{ title }}
    </p>
    <div class="text-p-sm opacity-90">
      <slot />
    </div>
  </div>
</template>
