<script setup lang="ts">
import { CircleCheckIcon, InfoIcon, OctagonXIcon, XIcon } from 'lucide-vue-next'

const { toasts, dismiss } = useToast()

const icons = { success: CircleCheckIcon, error: OctagonXIcon, info: InfoIcon }
const tones = {
  success: 'text-ink-green',
  error: 'text-ink-red',
  info: 'text-ink-blue',
}
</script>

<template>
  <div class="pointer-events-none fixed top-4 right-4 z-100 flex w-80 flex-col gap-2">
    <TransitionGroup
      enter-from-class="translate-x-4 opacity-0"
      enter-active-class="transition duration-150"
      leave-to-class="translate-x-4 opacity-0"
      leave-active-class="transition duration-150"
    >
      <div
        v-for="toast in toasts"
        :key="toast.id"
        class="pointer-events-auto flex items-start gap-2 rounded-lg border border-outline-gray-1 bg-popover p-3 text-base shadow-md"
      >
        <component :is="icons[toast.tone]" class="mt-0.5 size-4 shrink-0" :class="tones[toast.tone]" />
        <p class="flex-1 text-p-sm text-ink-gray-8">
          {{ toast.message }}
        </p>
        <button type="button" class="text-ink-gray-4 hover:text-ink-gray-7" @click="dismiss(toast.id)">
          <XIcon class="size-4" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
