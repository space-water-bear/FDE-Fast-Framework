import request from '@/utils/request'

export interface ChartDemoQuery {
  startDate: string
  endDate: string
  storeId?: string
}

export interface ChartDemoSales {
  source: 'demo'
  currency: 'CNY'
  amountUnit: 'cent'
  timezone: 'Asia/Shanghai'
  hasData: boolean
  dateRange: { startDate: string; endDate: string }
  stores: { id: string; name: string }[]
  totals: {
    salesCents: number
    refundCents: number
    netCents: number
    orderCount: number
  }
  trend: { date: string; netCents: number }[]
  comparison: { storeId: string; storeName: string; netCents: number }[]
  composition: { categoryId: string; categoryName: string; netCents: number }[]
}

export function getChartDemoSales(params: ChartDemoQuery) {
  return request.get<unknown, ChartDemoSales>('/demo/charts/sales', { params })
}
