<script setup lang="ts">
import { onClickOutside } from '@vueuse/core'

withDefaults(defineProps<{ align?: 'start' | 'end' }>(), { align: 'end' })

const open = ref(false)
const root = useTemplateRef<HTMLElement>('root')

onClickOutside(root, () => { open.value = false })
</script>

<template>
  <div ref="root" class="relative">
    <div :aria-expanded="open" @click="open = !open">
      <slot name="trigger" />
    </div>

    <div
      v-if="open"
      class="absolute z-50 mt-1 min-w-44 rounded-lg border border-outline-gray-1 bg-popover p-1 shadow-md"
      :class="align === 'end' ? 'right-0' : 'left-0'"
      role="menu"
      @click="open = false"
    >
      <slot />
    </div>
  </div>
</template>
