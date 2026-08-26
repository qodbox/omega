<template>
  <div class="relative w-full overflow-x-auto">
    <table
      class="w-full caption-bottom text-base
        [&_td]:px-3 [&_td]:align-middle [&_td]:whitespace-nowrap [&_td]:text-ink-gray-7
        [&_th]:h-9 [&_th]:px-3 [&_th]:text-left [&_th]:align-middle [&_th]:text-sm [&_th]:font-medium [&_th]:whitespace-nowrap [&_th]:text-ink-gray-5
        [&_tbody_tr]:h-10 [&_tbody_tr]:border-b [&_tbody_tr]:border-outline-gray-1 [&_tbody_tr]:transition-colors
        [&_tbody_tr:last-child]:border-0 [&_tbody_tr:hover]:bg-surface-gray-1
        [&_thead_tr]:border-b [&_thead_tr]:border-outline-gray-1"
    >
      <slot />
    </table>
  </div>
</template>
