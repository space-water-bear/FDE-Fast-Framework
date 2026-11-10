<template>
  <div class="base-chart" :style="{ height: cssHeight }" :aria-busy="loading" role="group" :aria-label="ariaLabel">
    <div
      ref="container"
      class="base-chart-canvas"
      :class="{ 'is-covered': !!status }"
      role="img"
      :aria-label="ariaLabel"
      :aria-hidden="!!status"
    />
    <div v-if="status" class="base-chart-status" :role="status === 'error' ? 'alert' : 'status'" aria-live="polite">
      <template v-if="status === 'loading'">
        <el-icon class="is-loading" :size="24" aria-hidden="true"><Loading /></el-icon>
        <p>图表加载中…</p>
      </template>
      <template v-else-if="status === 'error'">
        <p>{{ error }}</p>
        <el-button size="small" @click="emit('retry')">重新加载</el-button>
      </template>
      <p v-else>暂无数据</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, watch } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import type { ECElementEvent, EChartsOption, EChartsType } from 'echarts'
import { getInstanceByDom, init } from './runtime'

const props = withDefaults(defineProps<{
  option: EChartsOption
  height?: number | string
  loading?: boolean
  empty?: boolean
  error?: string
  ariaLabel?: string
}>(), {
  height: 320,
  loading: false,
  empty: false,
  error: '',
  ariaLabel: '统计图表',
})

const emit = defineEmits<{
  (event: 'click', params: ECElementEvent): void
  (event: 'ready', instance: EChartsType): void
  (event: 'retry'): void
}>()

const container = ref<HTMLDivElement>()
const cssHeight = computed(() => typeof props.height === 'number' ? `${props.height}px` : props.height)
const status = computed(() => props.loading ? 'loading' : props.error ? 'error' : props.empty ? 'empty' : '')

// Keep the ECharts instance outside Vue's reactive proxies.
let chart: EChartsType | undefined
let observer: ResizeObserver | undefined
let frame: number | undefined
let active = false
let optionDirty = true

function themeFor(element: HTMLElement) {
  const style = getComputedStyle(element)
  const css = (name: string, fallback: string) => style.getPropertyValue(name).trim() || fallback
  const textColor = css('--el-text-color-regular', '#606266')
  const borderColor = css('--el-border-color-lighter', '#ebeef5')
  return {
    color: [
      css('--el-color-primary', '#409eff'),
      css('--el-color-success', '#67c23a'),
      css('--el-color-warning', '#e6a23c'),
      css('--el-color-danger', '#f56c6c'),
      '#8b6fd1', '#2caca0',
    ],
    textStyle: { color: textColor, fontFamily: style.fontFamily },
    title: { textStyle: { color: css('--el-text-color-primary', '#303133') } },
    legend: { textStyle: { color: textColor } },
    categoryAxis: { axisLine: { lineStyle: { color: borderColor } }, axisLabel: { color: textColor } },
    valueAxis: { axisLabel: { color: textColor }, splitLine: { lineStyle: { color: borderColor } } },
  }
}

function renderChart() {
  frame = undefined
  const element = container.value
  if (!active || !element || status.value || element.clientWidth <= 0 || element.clientHeight <= 0) return
  let created = false
  if (!chart || chart.isDisposed()) {
    // The component owns this DOM node and its instance throughout its lifecycle.
    chart = getInstanceByDom(element) ?? init(element, themeFor(element), { renderer: 'canvas' })
    chart.on('click', (params: ECElementEvent) => emit('click', params))
    optionDirty = true
    created = true
  }
  chart.resize()
  if (optionDirty) {
    // Each prop value is a complete option. Replacing it also removes old series/axes.
    chart.setOption(props.option, { notMerge: true })
    optionDirty = false
  }
  if (created) emit('ready', chart)
}

function scheduleRender() {
  if (!active || frame !== undefined) return
  frame = requestAnimationFrame(renderChart)
}

function pause() {
  active = false
  if (frame !== undefined) cancelAnimationFrame(frame)
  frame = undefined
}

watch(() => props.option, () => {
  optionDirty = true
  scheduleRender()
}, { deep: true, flush: 'post' })
watch([status, cssHeight], scheduleRender, { flush: 'post' })

onMounted(() => {
  active = true
  if (container.value && typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(scheduleRender)
    observer.observe(container.value)
  } else {
    window.addEventListener('resize', scheduleRender)
  }
  scheduleRender()
})
onActivated(() => {
  active = true
  scheduleRender()
})
onDeactivated(pause)
onBeforeUnmount(() => {
  pause()
  observer?.disconnect()
  window.removeEventListener('resize', scheduleRender)
  chart?.dispose()
  chart = undefined
})

defineExpose({
  getInstance: () => chart,
  resize: scheduleRender,
})
</script>

<style scoped>
.base-chart {
  position: relative;
  width: 100%;
  min-width: 0;
}
.base-chart-canvas {
  width: 100%;
  height: 100%;
}
.base-chart-canvas.is-covered {
  visibility: hidden;
  pointer-events: none;
}
.base-chart-status {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 24px;
  box-sizing: border-box;
  text-align: center;
  color: var(--el-text-color-secondary);
}
.base-chart-status p {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
