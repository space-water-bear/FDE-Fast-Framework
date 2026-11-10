<template>
  <div class="page chart-demo-page">
    <div class="page-heading">
      <div>
        <h1>图表组件示例</h1>
        <p>折线图看趋势，柱状图做对比，环形图看构成。</p>
      </div>
      <el-tag type="info" effect="plain">示例数据</el-tag>
    </div>

    <el-alert
      title="数据说明"
      description="只读示例接口，数据固定在 2026-10-01 至 2026-10-30，不代表真实经营情况。净销售额 = 销售额 − 退款额；接口金额单位为分，图表展示为元；日期按 Asia/Shanghai 统计，包含首尾两天。"
      type="info"
      :closable="false"
      show-icon
    />

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="filters" @submit.prevent="loadReport">
        <el-form-item label="日期范围">
          <el-date-picker
            v-model="filters.dateRange"
            type="daterange"
            value-format="YYYY-MM-DD"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
            range-separator="至"
            :clearable="false"
            @change="loadReport"
          />
        </el-form-item>
        <el-form-item label="门店">
          <el-select v-model="filters.storeId" aria-label="门店" class="store-select" @change="loadReport">
            <el-option label="全部门店" value="all" />
            <el-option v-for="store in stores" :key="store.id" :label="store.name" :value="store.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading">查询</el-button>
          <el-button @click="resetFilters">重置</el-button>
        </el-form-item>
      </el-form>
      <p class="filter-help">选择“暂无交易门店”或数据范围外的日期，可查看空数据状态。点击门店柱形可筛选该门店。</p>
    </el-card>

    <div class="summary-grid" :aria-busy="loading">
      <el-card v-for="item in summaryItems" :key="item.label" shadow="never" class="summary-card">
        <span class="summary-label">{{ item.label }}</span>
        <strong>{{ item.value }}</strong>
      </el-card>
    </div>

    <p v-if="report && !loading && !error" class="report-scope" role="status">
      当前结果：{{ report.dateRange.startDate }} 至 {{ report.dateRange.endDate }} · {{ selectedStoreName }}
      <span v-if="!report.hasData"> · 暂无交易数据</span>
    </p>

    <el-card shadow="never" class="chart-card">
      <template #header>
        <div class="chart-heading"><h2>净销售额趋势</h2><span>按日汇总 · 元</span></div>
      </template>
      <base-chart
        :option="lineOption"
        :height="320"
        :loading="loading"
        :empty="empty"
        :error="error"
        aria-label="每日净销售额折线图"
        @retry="loadReport"
      />
    </el-card>

    <div class="chart-grid">
      <el-card shadow="never" class="chart-card">
        <template #header>
          <div class="chart-heading"><h2>门店净销售额</h2><span>当前筛选范围 · 元</span></div>
        </template>
        <base-chart
          :option="barOption"
          :height="320"
          :loading="loading"
          :empty="empty"
          :error="error"
          aria-label="门店净销售额柱状图，点击柱形筛选门店"
          @click="filterByStore"
          @retry="loadReport"
        />
      </el-card>
      <el-card shadow="never" class="chart-card">
        <template #header>
          <div class="chart-heading"><h2>分类净销售额构成</h2><span>当前筛选范围 · 元</span></div>
        </template>
        <base-chart
          :option="donutOption"
          :height="320"
          :loading="loading"
          :empty="empty"
          :error="error"
          aria-label="分类净销售额环形图"
          @retry="loadReport"
        />
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { getChartDemoSales } from '@/api/chart-demo'
import type { ChartDemoSales } from '@/api/chart-demo'
import { BaseChart, createBarOption, createDonutOption, createLineOption } from '@/components/charts'

const defaultRange: [string, string] = ['2026-10-01', '2026-10-30']
const filters = reactive({ dateRange: [...defaultRange] as [string, string], storeId: 'all' })
const report = ref<ChartDemoSales>()
const loading = ref(false)
const error = ref('')
// Retain filter choices when a request fails; only the latest request may publish a result.
const stores = ref<ChartDemoSales['stores']>([])
let requestSequence = 0

const currency = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' })
const money = (yuan: number) => currency.format(yuan)
const empty = computed(() => !report.value?.hasData)
const selectedStoreName = computed(() => stores.value.find((store) => store.id === filters.storeId)?.name || '全部门店')
const summaryItems = computed(() => {
  const totals = !loading.value && !error.value ? report.value?.totals : undefined
  return [
    { label: '销售额（元）', value: totals ? money(totals.salesCents / 100) : '—' },
    { label: '退款额（元）', value: totals ? money(totals.refundCents / 100) : '—' },
    { label: '净销售额（元）', value: totals ? money(totals.netCents / 100) : '—' },
    { label: '订单数', value: totals ? totals.orderCount.toLocaleString('zh-CN') : '—' },
  ]
})
const lineOption = computed(() => createLineOption({
  categories: report.value?.trend.map((item) => item.date) || [],
  series: [{ name: '净销售额', data: report.value?.trend.map((item) => item.netCents / 100) || [] }],
  valueName: '元',
  valueFormatter: money,
}))
const barOption = computed(() => createBarOption({
  categories: report.value?.comparison.map((item) => item.storeName) || [],
  series: [{ name: '净销售额', data: report.value?.comparison.map((item) => item.netCents / 100) || [] }],
  valueName: '元',
  valueFormatter: money,
}))
const donutOption = computed(() => createDonutOption({
  data: report.value?.composition.map((item) => ({ name: item.categoryName, value: item.netCents / 100 })) || [],
  seriesName: '净销售额',
  valueFormatter: money,
}))

async function loadReport() {
  const sequence = ++requestSequence
  if (!filters.dateRange?.[0] || !filters.dateRange?.[1]) {
    loading.value = false
    error.value = '请选择完整日期范围'
    return
  }
  loading.value = true
  error.value = ''
  try {
    const result = await getChartDemoSales({
      startDate: filters.dateRange[0],
      endDate: filters.dateRange[1],
      storeId: filters.storeId === 'all' ? '' : filters.storeId,
    })
    if (sequence !== requestSequence) return
    report.value = result
    stores.value = result.stores
  } catch (cause) {
    if (sequence !== requestSequence) return
    error.value = cause instanceof Error ? cause.message : '统计数据加载失败，请重试'
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function resetFilters() {
  filters.dateRange = [...defaultRange]
  filters.storeId = 'all'
  void loadReport()
}

function filterByStore(event: { name: string }) {
  const store = report.value?.comparison.find((item) => item.storeName === event.name)
  if (!store || filters.storeId === store.storeId) return
  filters.storeId = store.storeId
  void loadReport()
}

onMounted(loadReport)
onUnmounted(() => { requestSequence++ })
</script>

<style scoped>
.chart-demo-page { display: flex; flex-direction: column; gap: 16px; }
.page-heading, .chart-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.page-heading h1 { margin: 0; font-size: 22px; color: var(--el-text-color-primary); }
.page-heading p { margin: 8px 0 0; color: var(--el-text-color-secondary); font-size: 14px; }
.filter-card :deep(.el-card__body) { padding: 20px 20px 12px; }
.filter-card :deep(.el-form-item) { margin-bottom: 12px; }
.store-select { width: 180px; }
.filter-help, .report-scope { margin: 0; color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.6; }
.summary-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; }
.summary-card :deep(.el-card__body) { display: flex; flex-direction: column; gap: 12px; }
.summary-label { color: var(--el-text-color-secondary); font-size: 13px; }
.summary-card strong { color: var(--el-text-color-primary); font-size: 24px; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.chart-card { min-width: 0; }
.chart-card :deep(.el-card__header) { padding: 16px 20px; }
.chart-card :deep(.el-card__body) { padding: 12px 16px; }
.chart-heading h2 { margin: 0; font-size: 15px; color: var(--el-text-color-primary); }
.chart-heading span { color: var(--el-text-color-secondary); font-size: 12px; }
.chart-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
@media (max-width: 1100px) { .summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 850px) { .chart-grid { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 600px) {
  .summary-grid { grid-template-columns: minmax(0, 1fr); }
  .page-heading, .chart-heading { align-items: flex-start; flex-wrap: wrap; }
  .filter-card :deep(.el-form-item) { display: flex; margin-right: 0; }
  .filter-card :deep(.el-form-item__content) { min-width: 0; }
  .filter-card :deep(.el-date-editor) { width: 100%; min-width: 0; }
  .store-select { width: 100%; }
}
</style>
