import type { EChartsOption } from 'echarts'

export interface ChartSeries {
  name: string
  data: (number | null)[]
}

export interface CartesianChartConfig {
  categories: string[]
  series: ChartSeries[]
  valueName?: string
  valueFormatter?: (value: number) => string
}

export interface DonutChartConfig {
  data: { name: string; value: number }[]
  seriesName?: string
  valueFormatter?: (value: number) => string
}

const numberFormatter = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 })

function cartesianOption(config: CartesianChartConfig, type: 'line' | 'bar'): EChartsOption {
  const formatValue = config.valueFormatter ?? ((value: number) => numberFormatter.format(value))
  return {
    aria: { enabled: true },
    tooltip: {
      trigger: 'axis',
      valueFormatter: (value) => typeof value === 'number' ? formatValue(value) : String(value ?? '—'),
    },
    legend: { type: 'scroll', top: 0 },
    grid: { top: 50, right: 20, bottom: 16, left: 16, containLabel: true },
    xAxis: {
      type: 'category',
      boundaryGap: type === 'bar',
      data: [...config.categories],
      axisTick: { alignWithLabel: true },
      axisLabel: { hideOverlap: true },
    },
    yAxis: {
      type: 'value',
      name: config.valueName ?? '',
      axisLabel: { formatter: (value: number) => formatValue(value) },
    },
    series: config.series.map((series) => ({
      name: series.name,
      type,
      data: [...series.data],
      ...(type === 'line'
        ? { showSymbol: config.categories.length <= 14, connectNulls: false }
        : { barMaxWidth: 48 }),
    })),
  }
}

/** Data must already be aggregated and ordered by the caller's statistical contract. */
export function createLineOption(config: CartesianChartConfig): EChartsOption {
  return cartesianOption(config, 'line')
}

export function createBarOption(config: CartesianChartConfig): EChartsOption {
  return cartesianOption(config, 'bar')
}

export function createDonutOption(config: DonutChartConfig): EChartsOption {
  const formatValue = config.valueFormatter ?? ((value: number) => numberFormatter.format(value))
  return {
    aria: { enabled: true },
    tooltip: {
      trigger: 'item',
      valueFormatter: (value) => typeof value === 'number' ? formatValue(value) : String(value ?? '—'),
    },
    legend: { type: 'scroll', bottom: 0 },
    series: [{
      name: config.seriesName ?? '',
      type: 'pie',
      radius: ['42%', '68%'],
      center: ['50%', '44%'],
      stillShowZeroSum: false,
      label: { show: false },
      emphasis: { label: { show: true, formatter: '{b}\n{d}%' } },
      data: config.data.map((item) => ({ ...item })),
    }],
  }
}
