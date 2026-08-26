<script setup lang="ts">
const open = defineModel<boolean>('open', { default: false })
defineProps<{ title?: string, description?: string, wide?: boolean }>()

const element = useTemplateRef<HTMLDialogElement>('element')

watch(open, (shown) => {
  if (!element.value) return
  if (shown && !element.value.open) element.value.showModal()
  if (!shown && element.value.open) element.value.close()
})
</script>

<template>
  <dialog
    ref="element"
    class="m-auto w-[calc(100%-2rem)] rounded-xl border border-outline-gray-1 bg-popover p-0 text-base text-popover-foreground shadow-xl backdrop:bg-black/40 backdrop:backdrop-blur-xs"
    :class="wide ? 'sm:max-w-2xl' : 'sm:max-w-md'"
    @close="open = false"
    @click.self="open = false"
  >
    <div class="flex flex-col gap-4 p-4">
      <div v-if="title || description" class="grid gap-1">
        <h2 v-if="title" class="text-lg font-semibold text-ink-gray-9">
          {{ title }}
        </h2>
        <p v-if="description" class="text-p-sm text-ink-gray-5">
          {{ description }}
        </p>
      </div>
      <slot />
      <div v-if="$slots.footer" class="flex justify-end gap-2">
        <slot name="footer" />
      </div>
    </div>
  </dialog>
</template>
