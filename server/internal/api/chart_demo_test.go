package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

func TestChartDemoSalesResponseContract(t *testing.T) {
	router := gin.New()
	router.GET("/demo/charts/sales", ChartDemoSales)
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"?storeId=north&startDate=2026-10-01&endDate=2026-10-01", utils.CodeSuccess},
		{"?storeId=empty", utils.CodeSuccess},
		{"?startDate=2026-02-30", utils.CodeError},
		{"?storeId=unknown", utils.CodeError},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/demo/charts/sales"+tc.query, nil))
		var response struct {
			Code int                    `json:"code"`
			Msg  string                 `json:"msg"`
			Data service.ChartDemoSales `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusOK || response.Code != tc.code {
			t.Fatalf("query %s: HTTP %d response=%s", tc.query, recorder.Code, recorder.Body.String())
		}
		if tc.code == utils.CodeSuccess && (response.Data.Source != "demo" || response.Data.Trend == nil || response.Data.Comparison == nil || response.Data.Composition == nil) {
			t.Fatalf("missing explicit demo source or chart arrays: %s", recorder.Body.String())
		}
	}
}
