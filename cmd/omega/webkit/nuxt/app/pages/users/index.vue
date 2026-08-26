<script setup lang="ts">
import { refDebounced } from '@vueuse/core'
import { ChevronLeftIcon, ChevronRightIcon, SearchIcon, XIcon } from 'lucide-vue-next'
import { api } from '~/lib/api'

definePageMeta({ layout: 'dashboard', middleware: 'auth' })

const { t } = useI18n()

const term = ref('')
const page = ref(1)
const search = refDebounced(term, 250)

watch(search, () => { page.value = 1 })

const { data, pending } = await useAsyncData(
  'users',
  () => api.users({
    page: page.value,
    per_page: 10,
    sort: 'name',
    ...(search.value ? { name__like: `%${search.value}%` } : {}),
  }),
  { watch: [search, page] },
)

const meta = computed(() => data.value?.meta)
const rows = computed(() => data.value?.data ?? [])
</script>

<template>
  <UiCard :title="t('users.title')" :description="t('users.lead')">
    <div class="flex flex-col gap-4">
      <div class="relative max-w-sm">
        <SearchIcon class="absolute inset-y-0 left-2.5 my-auto size-4 text-ink-gray-4" />
        <UiInput v-model="term" :placeholder="t('users.filter')" class="px-8" />
        <UiButton
          v-if="term"
          variant="ghost"
          size="icon-sm"
          class="absolute inset-y-0 right-1 my-auto"
          :aria-label="t('users.clear')"
          @click="term = ''"
        >
          <XIcon />
        </UiButton>
      </div>

      <div v-if="pending && !rows.length" class="flex flex-col gap-2">
        <UiSkeleton v-for="index in 5" :key="index" class="h-10 w-full" />
      </div>

      <p v-else-if="!rows.length" class="py-12 text-center text-base text-ink-gray-5">
        {{ t('users.empty') }}
      </p>

      <UiTable v-else>
        <thead>
          <tr>
            <th>{{ t('users.name') }}</th>
            <th>{{ t('users.email') }}</th>
            <th>{{ t('users.role') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="user in rows" :key="user.id">
            <td>
              <NuxtLink :to="`/users/${user.id}`" class="font-medium text-ink-gray-9 hover:underline">
                {{ user.name }}
              </NuxtLink>
            </td>
            <td>{{ user.email }}</td>
            <td>
              <UiBadge :variant="user.role === 'admin' ? 'brand' : 'default'">
                {{ user.role }}
              </UiBadge>
            </td>
          </tr>
        </tbody>
      </UiTable>

      <div v-if="meta && meta.last_page > 1" class="flex items-center justify-between">
        <p class="text-sm text-ink-gray-5">
          {{ t('users.page', { page: meta.page, total: meta.last_page }) }}
        </p>
        <div class="flex gap-1">
          <UiButton variant="outline" size="icon-sm" :disabled="meta.page <= 1" @click="page--">
            <ChevronLeftIcon />
          </UiButton>
          <UiButton variant="outline" size="icon-sm" :disabled="meta.page >= meta.last_page" @click="page++">
            <ChevronRightIcon />
          </UiButton>
        </div>
      </div>
    </div>
  </UiCard>
</template>
