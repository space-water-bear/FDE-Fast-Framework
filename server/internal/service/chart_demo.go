package service

import (
	"errors"
	"time"
)

const (
	chartDemoStartDate = "2026-10-01"
	chartDemoEndDate   = "2026-10-30"
)

var chartDemoLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type ChartDemoStore struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ChartDemoDateRange struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

type ChartDemoTotals struct {
	SalesCents  int64 `json:"salesCents"`
	RefundCents int64 `json:"refundCents"`
	NetCents    int64 `json:"netCents"`
	OrderCount  int64 `json:"orderCount"`
}

type ChartDemoTrend struct {
	Date     string `json:"date"`
	NetCents int64  `json:"netCents"`
}

type ChartDemoComparison struct {
	StoreID   string `json:"storeId"`
	StoreName string `json:"storeName"`
	NetCents  int64  `json:"netCents"`
}

type ChartDemoComposition struct {
	CategoryID   string `json:"categoryId"`
	CategoryName string `json:"categoryName"`
	NetCents     int64  `json:"netCents"`
}

type ChartDemoSales struct {
	Source      string                 `json:"source"`
	Currency    string                 `json:"currency"`
	AmountUnit  string                 `json:"amountUnit"`
	Timezone    string                 `json:"timezone"`
	HasData     bool                   `json:"hasData"`
	DateRange   ChartDemoDateRange     `json:"dateRange"`
	Stores      []ChartDemoStore       `json:"stores"`
	Totals      ChartDemoTotals        `json:"totals"`
	Trend       []ChartDemoTrend       `json:"trend"`
	Comparison  []ChartDemoComparison  `json:"comparison"`
	Composition []ChartDemoComposition `json:"composition"`
}

type chartDemoQuery struct {
	start, end time.Time
	storeID    string
}

type chartDemoRecord struct {
	date, storeID, categoryID string
	salesCents, refundCents   int64
}

func chartDemoStores() []ChartDemoStore {
	return []ChartDemoStore{{"north", "北城店"}, {"south", "南城店"}, {"empty", "暂无交易门店"}}
}

func chartDemoCategories() []ChartDemoComposition {
	return []ChartDemoComposition{{CategoryID: "goods", CategoryName: "商品"}, {CategoryID: "service", CategoryName: "服务"}, {CategoryID: "other", CategoryName: "其他"}}
}

func parseChartDemoQuery(startDate, endDate, storeID string) (chartDemoQuery, error) {
	if startDate == "" {
		startDate = chartDemoStartDate
	}
	if endDate == "" {
		endDate = chartDemoEndDate
	}
	start, err := time.ParseInLocation(time.DateOnly, startDate, chartDemoLocation)
	if err != nil {
		return chartDemoQuery{}, errors.New("开始日期必须为有效的 YYYY-MM-DD 日期")
	}
	end, err := time.ParseInLocation(time.DateOnly, endDate, chartDemoLocation)
	if err != nil {
		return chartDemoQuery{}, errors.New("结束日期必须为有效的 YYYY-MM-DD 日期")
	}
	if end.Before(start) {
		return chartDemoQuery{}, errors.New("结束日期不能早于开始日期")
	}
	if end.Sub(start) > 365*24*time.Hour {
		return chartDemoQuery{}, errors.New("查询日期范围最多为 366 天（含首尾）")
	}
	if storeID != "" {
		found := false
		for _, store := range chartDemoStores() {
			if store.ID == storeID {
				found = true
				break
			}
		}
		if !found {
			return chartDemoQuery{}, errors.New("门店不存在")
		}
	}
	return chartDemoQuery{start: start, end: end, storeID: storeID}, nil
}

// GetChartDemoSales aggregates only deterministic in-memory examples, never customer data.
func GetChartDemoSales(startDate, endDate, storeID string) (*ChartDemoSales, error) {
	query, err := parseChartDemoQuery(startDate, endDate, storeID)
	if err != nil {
		return nil, err
	}
	return aggregateChartDemoSales(query, chartDemoRecords()), nil
}

func aggregateChartDemoSales(query chartDemoQuery, records []chartDemoRecord) *ChartDemoSales {
	startDate, endDate := query.start.Format(time.DateOnly), query.end.Format(time.DateOnly)
	result := &ChartDemoSales{
		Source: "demo", Currency: "CNY", AmountUnit: "cent", Timezone: "Asia/Shanghai",
		DateRange: ChartDemoDateRange{StartDate: startDate, EndDate: endDate},
		Stores:    chartDemoStores(), Trend: []ChartDemoTrend{}, Comparison: []ChartDemoComparison{},
		Composition: chartDemoCategories(),
	}
	dateIndex := make(map[string]int)
	for day := query.start; !day.After(query.end); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		dateIndex[date] = len(result.Trend)
		result.Trend = append(result.Trend, ChartDemoTrend{Date: date})
	}
	storeIndex := make(map[string]int)
	for _, store := range result.Stores {
		if query.storeID != "" && store.ID != query.storeID {
			continue
		}
		storeIndex[store.ID] = len(result.Comparison)
		result.Comparison = append(result.Comparison, ChartDemoComparison{StoreID: store.ID, StoreName: store.Name})
	}
	categoryIndex := make(map[string]int)
	for i, category := range result.Composition {
		categoryIndex[category.CategoryID] = i
	}
	for _, record := range records {
		if record.date < startDate || record.date > endDate || (query.storeID != "" && query.storeID != record.storeID) {
			continue
		}
		net := record.salesCents - record.refundCents
		result.HasData = true
		result.Totals.SalesCents += record.salesCents
		result.Totals.RefundCents += record.refundCents
		result.Totals.NetCents += net
		result.Totals.OrderCount++
		result.Trend[dateIndex[record.date]].NetCents += net
		result.Comparison[storeIndex[record.storeID]].NetCents += net
		result.Composition[categoryIndex[record.categoryID]].NetCents += net
	}
	return result
}

// Each row is one synthetic order. Missing transaction days intentionally exercise zero filling.
func chartDemoRecords() []chartDemoRecord {
	start, _ := time.ParseInLocation(time.DateOnly, chartDemoStartDate, chartDemoLocation)
	stores := chartDemoStores()[:2]
	categories := chartDemoCategories()
	records := make([]chartDemoRecord, 0, 30*len(stores)*len(categories))
	for day := 1; day <= 30; day++ {
		if day%7 == 0 {
			continue
		}
		date := start.AddDate(0, 0, day-1).Format(time.DateOnly)
		for storeIndex, store := range stores {
			for categoryIndex, category := range categories {
				sales := int64(2000 + (day*137+storeIndex*719+categoryIndex*367)%10000)
				var refund int64
				if (day+storeIndex+categoryIndex)%5 == 0 {
					refund = sales / 10
				}
				records = append(records, chartDemoRecord{date: date, storeID: store.ID, categoryID: category.CategoryID, salesCents: sales, refundCents: refund})
			}
		}
	}
	return records
}
