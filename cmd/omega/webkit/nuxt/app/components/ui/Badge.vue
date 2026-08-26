<script setup lang="ts">
const props = withDefaults(defineProps<{
  variant?: 'default' | 'brand' | 'success' | 'warning' | 'info' | 'destructive' | 'outline'
  shape?: 'default' | 'pill' | 'dot'
}>(), { variant: 'default', shape: 'default' })

const variants = {
  default: 'bg-surface-gray-2 text-ink-gray-7',
  brand: 'bg-surface-brand-2 text-ink-brand',
  success: 'bg-surface-green-2 text-ink-green',
  warning: 'bg-surface-amber-2 text-ink-amber',
  info: 'bg-surface-blue-2 text-ink-blue',
  destructive: 'bg-surface-red-2 text-ink-red',
  outline: 'border border-outline-gray-2 text-ink-gray-7',
}

const shapes = {
  default: 'h-5 rounded-sm px-1.5',
  pill: 'h-5 rounded-full px-2',
  dot: 'size-2 rounded-full',
}
</script>

<template>
  <span
    class="inline-flex w-fit shrink-0 items-center justify-center gap-1 border border-transparent text-xs font-medium whitespace-nowrap [&>svg]:size-3"
    :class="[variants[props.variant], shapes[props.shape]]"
  >
    <slot />
  </span>
</template>
