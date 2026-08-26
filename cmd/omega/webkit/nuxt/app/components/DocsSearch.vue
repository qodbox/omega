<script setup lang="ts">
import { SearchIcon } from 'lucide-vue-next'

import type { DocPage } from '~/lib/docs'

const { t, loadDocs } = useI18n()
const open = ref(false)
const term = ref('')

// The docs content is heavy; it has no business in the home-page bundle.
const docs = ref<DocPage[]>([])
const sections = ref<{ key: string, title: string }[]>([])

async function load() {
  if (docs.value.length) return
  await loadDocs()
  const module = await import('~/lib/docs')
  docs.value = module.docs
  sections.value = module.sections
}

watch(open, (shown) => { if (shown) void load() })

const results = computed(() => {
  const needle = term.value.trim().toLowerCase()
  if (!needle) return docs.value

  return docs.value.filter((page) => {
    const haystack = [page.title, page.lead, ...page.blocks.map(block => block.heading ?? '')]
    return haystack.join(' ').toLowerCase().includes(needle)
  })
})

const grouped = computed(() =>
  sections.value
    .map(section => ({ section, pages: results.value.filter(page => page.section === section.key) }))
    .filter(entry => entry.pages.length > 0),
)

function go(slug: string) {
  open.value = false
  term.value = ''
  navigateTo(`/docs/${slug}`)
}

function onKey(event: KeyboardEvent) {
  if (event.key.toLowerCase() !== 'k' || !(event.metaKey || event.ctrlKey)) return
  event.preventDefault()
  open.value = !open.value
}

onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <UiButton
    variant="outline"
    size="sm"
    class="text-ink-gray-5 md:w-56 md:justify-start"
    :aria-label="t('docs.search')"
    @click="open = true"
  >
    <SearchIcon />
    <span class="hidden md:inline">{{ t('docs.search') }}</span>
  </UiButton>

  <UiDialog v-model:open="open" wide :title="t('docs.search')" :description="t('docs.browse')">
    <UiInput v-model="term" :placeholder="t('docs.search')" autofocus />
    <div class="max-h-80 overflow-y-auto">
      <p v-if="!grouped.length" class="py-8 text-center text-base text-ink-gray-5">
        {{ t('docs.noResult') }}
      </p>

      <div v-for="entry in grouped" :key="entry.section.key" class="py-1">
        <p class="px-2 py-1.5 text-xs font-medium text-ink-gray-5">
          {{ t(`docSection.${entry.section.key}`, entry.section.title) }}
        </p>
        <button
          v-for="page in entry.pages"
          :key="page.slug"
          type="button"
          class="flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-surface-gray-2"
          @click="go(page.slug)"
        >
          <span class="truncate text-base text-ink-gray-8">{{ t(`doc.${page.slug}.title`, page.title) }}</span>
          <span class="truncate text-xs text-ink-gray-5">{{ t(`doc.${page.slug}.lead`, page.lead) }}</span>
        </button>
      </div>
    </div>
  </UiDialog>
</template>
