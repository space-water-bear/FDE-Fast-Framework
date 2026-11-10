package service

import (
	"testing"
)

func TestChartDemoAggregationUsesOneFilteredDataset(t *testing.T) {
	query, err := parseChartDemoQuery("2026-10-01", "2026-10-03", "north")
	if err != nil {
		t.Fatal(err)
	}
	records := []chartDemoRecord{
		{date: "2026-10-03", storeID: "north", categoryID: "service", salesCents: 1001, refundCents: 201},
		{date: "2026-10-01", storeID: "north", categoryID: "goods", salesCents: 99, refundCents: 9},
		{date: "2026-10-02", storeID: "south", categoryID: "goods", salesCents: 5000},
		{date: "2026-09-30", storeID: "north", categoryID: "other", salesCents: 7000},
		{date: "2026-10-04", storeID: "north", categoryID: "other", salesCents: 8000},
	}
	got := aggregateChartDemoSales(query, records)
	if !got.HasData || got.Totals != (ChartDemoTotals{SalesCents: 1100, RefundCents: 210, NetCents: 890, OrderCount: 2}) {
		t.Fatalf("incorrect filtered totals: %+v", got.Totals)
	}
	if len(got.Trend) != 3 || got.Trend[0] != (ChartDemoTrend{Date: "2026-10-01", NetCents: 90}) || got.Trend[1] != (ChartDemoTrend{Date: "2026-10-02"}) || got.Trend[2] != (ChartDemoTrend{Date: "2026-10-03", NetCents: 800}) {
		t.Fatalf("trend must include endpoints, sort and zero fill: %+v", got.Trend)
	}
	if len(got.Comparison) != 1 || got.Comparison[0].StoreID != "north" || got.Comparison[0].NetCents != 890 {
		t.Fatalf("comparison not filtered consistently: %+v", got.Comparison)
	}
	if got.Composition[0].NetCents != 90 || got.Composition[1].NetCents != 800 || got.Composition[2].NetCents != 0 {
		t.Fatalf("incorrect composition: %+v", got.Composition)
	}
}

func TestChartDemoFixtureTotalsAndFilters(t *testing.T) {
	all, err := GetChartDemoSales("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if all.Source != "demo" || all.AmountUnit != "cent" || all.Currency != "CNY" || all.Timezone != "Asia/Shanghai" || len(all.Stores) != 3 || all.DateRange.StartDate != chartDemoStartDate || all.DateRange.EndDate != chartDemoEndDate {
		t.Fatalf("incorrect source/units/default range: %+v", all)
	}
	if all.Totals.OrderCount != 156 || all.Totals.RefundCents <= 0 || all.Totals.NetCents != all.Totals.SalesCents-all.Totals.RefundCents {
		t.Fatalf("incorrect transaction/refund accounting: %+v", all.Totals)
	}
	var trendNet, comparisonNet, compositionNet int64
	for _, row := range all.Trend {
		trendNet += row.NetCents
	}
	for _, row := range all.Comparison {
		comparisonNet += row.NetCents
	}
	for _, row := range all.Composition {
		compositionNet += row.NetCents
	}
	if trendNet != all.Totals.NetCents || comparisonNet != trendNet || compositionNet != trendNet {
		t.Fatalf("chart totals drift: summary=%d trend=%d comparison=%d composition=%d", all.Totals.NetCents, trendNet, comparisonNet, compositionNet)
	}
	north, err := GetChartDemoSales("", "", "north")
	if err != nil {
		t.Fatal(err)
	}
	south, err := GetChartDemoSales("", "", "south")
	if err != nil {
		t.Fatal(err)
	}
	if north.Totals.OrderCount != 78 || north.Totals.NetCents+south.Totals.NetCents != all.Totals.NetCents {
		t.Fatal("store filters do not partition the full dataset")
	}
}

func TestChartDemoNoDataAndZeroAreDistinct(t *testing.T) {
	for _, filter := range []struct{ start, end, store string }{
		{"2026-10-07", "2026-10-07", ""},
		{"2026-11-01", "2026-11-03", ""},
		{"", "", "empty"},
	} {
		got, err := GetChartDemoSales(filter.start, filter.end, filter.store)
		if err != nil {
			t.Fatal(err)
		}
		if got.HasData || got.Totals != (ChartDemoTotals{}) || len(got.Trend) == 0 {
			t.Fatalf("expected empty transaction result with zero-filled trend: %+v", got)
		}
	}
	query, err := parseChartDemoQuery("2026-10-01", "2026-10-01", "north")
	if err != nil {
		t.Fatal(err)
	}
	got := aggregateChartDemoSales(query, []chartDemoRecord{{date: "2026-10-01", storeID: "north", categoryID: "goods", salesCents: 100, refundCents: 100}})
	if !got.HasData || got.Totals.NetCents != 0 || got.Totals.OrderCount != 1 {
		t.Fatalf("fully refunded transaction must remain data: %+v", got)
	}
}

func TestChartDemoQueryValidation(t *testing.T) {
	for _, tc := range []struct {
		name, start, end, store string
	}{
		{"invalid start", "2026-02-30", "2026-10-30", ""},
		{"invalid end", "2026-10-01", "2026/10/30", ""},
		{"reverse range", "2026-10-02", "2026-10-01", ""},
		{"too many days", "2026-01-01", "2027-01-02", ""},
		{"unknown store", "", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := GetChartDemoSales(tc.start, tc.end, tc.store); err == nil {
				t.Fatal("expected invalid filter to be rejected")
			}
		})
	}
	got, err := GetChartDemoSales("2026-01-01", "2027-01-01", "")
	if err != nil || len(got.Trend) != 366 {
		t.Fatalf("366 inclusive days should be accepted: got=%v err=%v", got, err)
	}
}
