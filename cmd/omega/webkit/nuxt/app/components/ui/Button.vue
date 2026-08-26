<script setup lang="ts">
const props = withDefaults(defineProps<{
  variant?: 'default' | 'brand' | 'secondary' | 'outline' | 'ghost' | 'destructive' | 'link'
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'icon' | 'icon-sm'
  as?: string
}>(), { variant: 'default', size: 'md', as: 'button' })

const variants = {
  default: 'bg-surface-gray-10 text-ink-base shadow-raised hover:bg-surface-gray-9 active:bg-surface-gray-8',
  brand: 'bg-brand text-brand-foreground shadow-raised hover:bg-brand-7 active:bg-brand-8',
  secondary: 'bg-surface-gray-2 text-ink-gray-8 hover:bg-surface-gray-3 active:bg-surface-gray-4',
  outline: 'border border-outline-gray-2 bg-surface-base text-ink-gray-8 shadow-2xs hover:border-outline-gray-3 hover:bg-surface-gray-1',
  ghost: 'text-ink-gray-8 hover:bg-surface-gray-3 active:bg-surface-gray-4',
  destructive: 'bg-surface-red-7 text-ink-base shadow-raised hover:bg-surface-red-6',
  link: 'text-ink-link underline-offset-4 hover:underline',
}

const sizes = {
  'xs': 'h-6 gap-1 rounded-sm px-1.5 text-xs',
  'sm': 'h-7 gap-1.5 rounded-sm px-2 text-sm',
  'md': 'h-8 gap-1.5 rounded-md px-2.5 text-base',
  'lg': 'h-10 gap-2 rounded-lg px-3.5 text-md',
  'icon': 'size-8 rounded-md',
  'icon-sm': 'size-7 rounded-sm',
}
</script>

<template>
  <component
    :is="props.as"
    class="inline-flex shrink-0 items-center justify-center border border-transparent font-medium whitespace-nowrap outline-none transition-[color,background-color,border-color,box-shadow] duration-100 select-none focus-visible:ring-2 focus-visible:ring-ring/60 focus-visible:ring-offset-1 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-55 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*=size-])]:size-4"
    :class="[variants[props.variant], sizes[props.size]]"
  >
    <slot />
  </component>
</template>
