<script setup lang="ts">
withDefaults(defineProps<{ orientation?: 'horizontal' | 'vertical' }>(), { orientation: 'horizontal' })
</script>

<template>
  <div
    role="separator"
    class="shrink-0 bg-outline-gray-1"
    :class="orientation === 'vertical' ? 'w-px self-stretch' : 'h-px w-full'"
  />
</template>
