<script setup lang="ts">
import {
  BookOpenIcon,
  LayoutDashboardIcon,
  LogOutIcon,
  NetworkIcon,
  UsersIcon,
  XIcon,
} from 'lucide-vue-next'

const { t } = useI18n()
const { user, signOut } = useAuth()
const { collapsed, mobileOpen } = useSidebar()

const nav = computed(() => [
  { label: t('nav.dashboard'), to: '/dashboard', icon: LayoutDashboardIcon },
  { label: t('nav.users'), to: '/users', icon: UsersIcon },
])

const resources = computed(() => [
  { label: t('nav.docs'), href: '/docs', icon: BookOpenIcon, external: false },
  { label: t('nav.rest'), href: '/api/docs', icon: NetworkIcon, external: true },
  { label: t('nav.graphql'), href: '/graphql', icon: NetworkIcon, external: true },
])

// Collapsing, the text leaves first and fast; expanding, it comes back once
// the width is nearly there. That offset is what makes the gesture smooth.
const label = computed(() =>
  collapsed.value
    ? 'pointer-events-none opacity-0 duration-100'
    : 'opacity-100 delay-150 duration-200',
)

async function leave() {
  await signOut()
  navigateTo('/login')
}
</script>

<template>
  <Transition
    enter-from-class="opacity-0"
    enter-active-class="transition-opacity duration-200"
    leave-to-class="opacity-0"
    leave-active-class="transition-opacity duration-200"
  >
    <div
      v-if="mobileOpen"
      class="fixed inset-0 z-40 bg-black/40 md:hidden"
      @click="mobileOpen = false"
    />
  </Transition>

  <aside
    class="fixed inset-y-0 left-0 z-50 flex h-svh shrink-0 flex-col border-r border-outline-gray-1 bg-sidebar transition-[width,transform] duration-300 ease-spring will-change-[width] md:sticky md:top-0 md:translate-x-0"
    :class="[
      collapsed ? 'w-sidebar md:w-rail' : 'w-sidebar',
      mobileOpen ? 'translate-x-0' : '-translate-x-full',
    ]"
  >
    <div class="flex h-header shrink-0 items-center gap-2 overflow-hidden px-2">
      <NuxtLink
        to="/"
        class="flex min-w-0 items-center gap-2 rounded-md px-1 font-semibold whitespace-nowrap text-ink-gray-9"
        :title="collapsed ? 'Omega' : undefined"
      >
        <span class="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary text-sm font-bold text-primary-foreground">Ω</span>
        <span class="truncate transition-opacity" :class="label">Omega</span>
      </NuxtLink>

      <UiButton
        variant="ghost"
        size="icon-sm"
        class="ml-auto md:hidden"
        :aria-label="t('nav.menu')"
        @click="mobileOpen = false"
      >
        <XIcon />
      </UiButton>
    </div>

    <nav class="flex flex-1 flex-col gap-0.5 overflow-x-hidden overflow-y-auto px-2 pb-10">
      <NuxtLink
        v-for="item in nav"
        :key="item.to"
        :to="item.to"
        :title="collapsed ? item.label : undefined"
        class="flex h-8 shrink-0 items-center gap-2 overflow-hidden rounded-md px-2 text-base whitespace-nowrap text-ink-gray-7 transition-colors duration-150 hover:bg-surface-gray-3 hover:text-ink-gray-9"
        active-class="bg-surface-gray-3 font-medium text-ink-gray-9 [&_svg]:text-brand"
        @click="mobileOpen = false"
      >
        <component :is="item.icon" class="size-4 shrink-0" />
        <span class="truncate transition-opacity" :class="label">{{ item.label }}</span>
      </NuxtLink>

      <p
        class="mt-4 shrink-0 overflow-hidden px-2 py-1.5 text-xs font-medium whitespace-nowrap text-ink-gray-5 transition-opacity"
        :class="label"
      >
        {{ t('nav.docs') }}
      </p>

      <a
        v-for="item in resources"
        :key="item.href"
        :href="item.href"
        :target="item.external ? '_blank' : undefined"
        :rel="item.external ? 'noreferrer' : undefined"
        :title="collapsed ? item.label : undefined"
        class="flex h-8 shrink-0 items-center gap-2 overflow-hidden rounded-md px-2 text-base whitespace-nowrap text-ink-gray-7 transition-colors duration-150 hover:bg-surface-gray-3 hover:text-ink-gray-9"
      >
        <component :is="item.icon" class="size-4 shrink-0" />
        <span class="truncate transition-opacity" :class="label">{{ item.label }}</span>
      </a>
    </nav>

    <!-- No overflow-hidden here: the profile menu has to escape the box. -->
    <div v-if="user" class="shrink-0 border-t border-outline-gray-1 p-2">
      <UiDropdown align="start">
        <template #trigger>
          <button
            type="button"
            class="flex w-full items-center gap-2 rounded-md p-1.5 text-left whitespace-nowrap transition-colors duration-150 hover:bg-surface-gray-3"
            :title="collapsed ? user.name : undefined"
          >
            <UiAvatar :name="user.name" class="size-7 shrink-0" />
            <span class="min-w-0 flex-1 overflow-hidden transition-opacity" :class="label">
              <span class="block truncate text-sm font-medium text-ink-gray-9">{{ user.name }}</span>
              <span class="block truncate text-xs text-ink-gray-5">{{ user.email }}</span>
            </span>
          </button>
        </template>

        <UiDropdownItem @click="leave">
          <LogOutIcon />
          {{ t('nav.signOut') }}
        </UiDropdownItem>
      </UiDropdown>
    </div>
  </aside>
</template>
