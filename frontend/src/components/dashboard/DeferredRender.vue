<template>
  <div ref="root" class="deferred-render">
    <slot v-if="active" />
    <slot v-else name="placeholder" />
  </div>
</template>

<script lang="ts" setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(defineProps<{
  once?: boolean
  rootMargin?: string
}>(), {
  once: true,
  rootMargin: '160px 0px',
})

const active = ref(false)
const root = ref<HTMLElement | null>(null)

let observer: IntersectionObserver | null = null

const activate = () => {
  active.value = true
  if (props.once) {
    observer?.disconnect()
    observer = null
  }
}

onMounted(() => {
  if (typeof IntersectionObserver === 'undefined' || !root.value) {
    activate()
    return
  }

  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) {
      activate()
    }
  }, {
    rootMargin: props.rootMargin,
  })

  observer.observe(root.value)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})
</script>

<style scoped>
.deferred-render {
  min-height: inherit;
}
</style>
