<template>
  <div class="page usage-page">
    <div class="stat-cards">
      <div class="stat-card">
        <div class="stat-label">总调用次数</div>
        <div class="stat-value">{{ summary.totalCalls }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">总 Token 消耗</div>
        <div class="stat-value">{{ summary.totalTokens.toLocaleString() }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">平均延迟</div>
        <div class="stat-value">{{ avgLatency }} ms</div>
      </div>
    </div>

    <div class="group-row">
      <div class="group-card">
        <div class="group-title">按供应商</div>
        <el-table :data="summary.byProvider" size="small" border>
          <el-table-column prop="name" label="供应商" min-width="120" />
          <el-table-column prop="calls" label="调用次数" width="100" />
          <el-table-column label="Token" width="120">
            <template #default="{ row }">{{ row.tokens.toLocaleString() }}</template>
          </el-table-column>
        </el-table>
      </div>
      <div class="group-card">
        <div class="group-title">按智能体</div>
        <el-table :data="summary.byAgent" size="small" border>
          <el-table-column prop="name" label="智能体" min-width="120" />
          <el-table-column prop="calls" label="调用次数" width="100" />
          <el-table-column label="Token" width="120">
            <template #default="{ row }">{{ row.tokens.toLocaleString() }}</template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <div class="list-card">
      <div class="list-toolbar">
        <el-select v-model="filterProviderId" placeholder="全部供应商" clearable style="width: 200px" @change="handleFilter">
          <el-option v-for="p in providers" :key="p.id" :label="p.name" :value="p.id" />
        </el-select>
        <el-select v-model="filterAgentId" placeholder="全部智能体" clearable style="width: 200px" @change="handleFilter">
          <el-option v-for="a in agents" :key="a.id" :label="a.name" :value="a.id" />
        </el-select>
        <el-button :icon="Refresh" @click="loadAll">刷新</el-button>
      </div>
      <el-table v-loading="loading" :data="usages" size="small" border>
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="createdAt" label="时间" min-width="160" />
        <el-table-column prop="model" label="模型" min-width="140" />
        <el-table-column prop="promptTokens" label="输入Token" width="100" />
        <el-table-column prop="completionTokens" label="输出Token" width="100" />
        <el-table-column label="总Token" width="100">
          <template #default="{ row }">{{ row.totalTokens.toLocaleString() }}</template>
        </el-table-column>
        <el-table-column prop="latencyMs" label="延迟(ms)" width="100" />
      </el-table>
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        small
        class="list-pagination"
        @current-change="loadUsages"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { getAiUsageSummary, listAiAgents, listAiProviders, listAiUsages } from '@/api/ai'
import type { AiAgentItem, AiProviderItem, AiUsageItem, AiUsageSummary } from '@/api/ai'

const loading = ref(false)
const summary = reactive<AiUsageSummary>({ totalCalls: 0, totalTokens: 0, totalLatencyMs: 0, byProvider: [], byAgent: [] })
const providers = ref<AiProviderItem[]>([])
const agents = ref<AiAgentItem[]>([])
const usages = ref<AiUsageItem[]>([])
const page = ref(1)
const pageSize = 20
const total = ref(0)
const filterProviderId = ref<number | undefined>(undefined)
const filterAgentId = ref<number | undefined>(undefined)

const avgLatency = computed(() => (summary.totalCalls > 0 ? Math.round(summary.totalLatencyMs / summary.totalCalls) : 0))

async function loadSummary() {
  const data = await getAiUsageSummary()
  Object.assign(summary, data)
}

async function loadUsages() {
  loading.value = true
  try {
    const data = await listAiUsages({
      page: page.value,
      pageSize,
      agentId: filterAgentId.value,
      providerId: filterProviderId.value,
    })
    usages.value = data.list
    total.value = data.total
  } finally {
    loading.value = false
  }
}

function handleFilter() {
  page.value = 1
  loadUsages()
}

async function loadAll() {
  await Promise.all([loadSummary(), loadUsages()])
}

onMounted(async () => {
  const [ps, as] = await Promise.all([listAiProviders(), listAiAgents()])
  providers.value = ps.list
  agents.value = as.list
  await loadAll()
})
</script>

<style scoped>
.usage-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.stat-cards {
  display: flex;
  gap: 12px;
}
.stat-card {
  flex: 1;
  background: #fff;
  border-radius: 4px;
  padding: 16px;
}
.stat-label {
  color: #909399;
  font-size: 13px;
}
.stat-value {
  font-size: 26px;
  font-weight: 600;
  margin-top: 6px;
}
.group-row {
  display: flex;
  gap: 12px;
}
.group-card {
  flex: 1;
  background: #fff;
  border-radius: 4px;
  padding: 12px;
}
.group-title {
  font-weight: 600;
  margin-bottom: 8px;
}
.list-card {
  background: #fff;
  border-radius: 4px;
  padding: 12px;
}
.list-toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}
.list-pagination {
  margin-top: 10px;
  justify-content: flex-end;
}
</style>
