<script lang="ts" setup>
import { HumanReadable } from '@/plugins/utils'
import { computed } from 'vue'

const props = defineProps({
  tilesData: <any>{},
  type: String
})

const data = computed(() => {
  const d = props.tilesData
  if (!d.mem && !d.cpu) return emptyGaugeData()
  switch (props.type) {
    case 'g-cpu':
      return { percent: d.cpu, primary: Math.ceil(d.cpu) + '%', ratio: false }
    case 'g-mem':
      return gaugeData(d.mem)
    case 'g-dsk':
      return gaugeData(d.dsk)
    case 'g-swp':
      return gaugeData(d.swp)
  }
  return emptyGaugeData()
})

const emptyGaugeData = () => ({ percent: 0, primary: '-', ratio: false })

const gaugeData = (d:any) :any => {
  if (!d || !Number.isFinite(d.current) || !Number.isFinite(d.total) || d.total <= 0) {
    return emptyGaugeData()
  }
  const curr = HumanReadable.sizeFormat(d.current,0).split(' ')
  const total = HumanReadable.sizeFormat(d.total,0).split(' ')
  if (curr[1] == total[1]) curr[1] = ''
  return {
    percent: Math.ceil(d.current*100/d.total),
    primary: curr[0],
    primaryUnit: curr[1] ?? '',
    secondary: total[0],
    secondaryUnit: total[1] ?? '',
    ratio: true
  }
}

const cssTransformRotateValue = computed(() => {
  const percentageAsFraction = data.value.percent / 100
  const halfPercentage = percentageAsFraction / 2

  return `${halfPercentage}turn`
})

const gaugeColor = computed(() => {
  if (data.value.percent > 90) return 'error'
  if (data.value.percent > 70) return 'warning'
  return 'info'
})
</script>

<template>
  <div class="gauge__outer">
    <div class="gauge__inner">
      <div
        class="gauge__fill" 
        :style="{ 
          transform: `rotate(${cssTransformRotateValue})`,
          background: `rgb(var(--v-theme-${gaugeColor}))`
          }">
      </div>
      <div class="gauge__cover">
        <span dir="ltr">
          <template v-if="data.ratio">
            {{ data.primary }}<sup>{{ data.primaryUnit }}</sup>/{{ data.secondary }}<sup>{{ data.secondaryUnit }}</sup>
          </template>
          <template v-else>{{ data.primary }}</template>
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.gauge__outer {
  width: 100%;
  max-width: 250px;
}

.gauge__inner {
  width: 100%;
  height: 0;
  padding-bottom: 50%;
  background: rgb(var(--v-theme-surface));
  position: relative;
  border-top-left-radius: 100% 200%;
  border-top-right-radius: 100% 200%;
  overflow: hidden;
}

.gauge__fill {
  position: absolute;
  top: 100%;
  left: 0;
  width: inherit;
  height: 100%;
  background: rgb(var(--v-theme-primary));
  transform-origin: center top;
  transform: rotate(0turn);
  transition: transform 200ms var(--np-ease-out);
}

.gauge__cover {
  width: 75%;
  height: 150%;
  background: rgb(var(--v-theme-background));
  position: absolute;
  top: 25%;
  left: 50%;
  transform: translateX(-50%);
  border-radius: 50%;

  /* Text */
  display: flex;
  align-items: center;
  justify-content: center;
  padding-bottom: 25%;
  box-sizing: border-box;
  font-family: 'Lexend', sans-serif;
  font-weight: bold;
  font-size: 32px;
}

sup {
  font-size: 16px;
}

@media (prefers-reduced-motion: reduce) {
  .gauge__fill {
    transition: none;
  }
}
</style>
