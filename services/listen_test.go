package services

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"

	"apple-store-helper/model"
)

func TestAddStoresAddsOneListenItemPerStore(t *testing.T) {
	area := model.Areas[0]
	storeTitles := Store.ByAreaTitleForOptions(area.Title)
	productTitles := Product.ByAreaTitleForOptions(area.Title)
	if len(storeTitles) < 2 || len(productTitles) == 0 {
		t.Fatal("test configuration needs at least two stores and one product")
	}

	service := listenService{
		items:  make(map[string]ListenItem),
		Status: binding.NewString(),
		Area:   area,
		Logs:   widget.NewLabel(""),
	}
	selectedStores := storeTitles[:2]
	service.AddStores(area.Title, selectedStores, productTitles[0], "https://example.test/key")

	if got := len(service.GetListenItems()); got != len(selectedStores) {
		t.Fatalf("expected %d listen items, got %d", len(selectedStores), got)
	}
	if service.BarkNotifyUrl != "https://example.test/key" {
		t.Fatalf("Bark URL was not retained")
	}
	logText := service.Logs.Text
	if matched, _ := regexp.MatchString(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\] \[等待\]`, logText); !matched {
		t.Fatalf("log line does not start with the expected timestamp: %q", logText)
	}

	service.AddStores(area.Title, selectedStores, productTitles[0], service.BarkNotifyUrl)
	if got := len(service.GetListenItems()); got != len(selectedStores) {
		t.Fatalf("adding the same stores again created duplicates: got %d items", got)
	}
}

func TestSetListenItemsRepairsLegacyEmptyTime(t *testing.T) {
	service := listenService{
		items:  make(map[string]ListenItem),
		Status: binding.NewString(),
		Logs:   widget.NewLabel(""),
	}
	service.SetListenItems(map[string]ListenItem{
		"legacy": {
			Status:  StatusWait,
			Store:   model.Store{CityStoreName: "Legacy Store"},
			Product: model.Product{Title: "Legacy Product"},
		},
	})

	if matched, _ := regexp.MatchString(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\] \[等待\]`, service.Logs.Text); !matched {
		t.Fatalf("legacy log line does not contain a repaired timestamp: %q", service.Logs.Text)
	}
}

func TestUpdateLogStrFallsBackWhenTimeIsInvalid(t *testing.T) {
	service := listenService{
		items: map[string]ListenItem{
			"invalid": {
				Status:       StatusError,
				Store:        model.Store{CityStoreName: "Test Store"},
				Product:      model.Product{Title: "Test Product"},
				ErrorMessage: "connection failed",
			},
		},
		Logs: widget.NewLabel(""),
	}
	service.UpdateLogStr()

	if matched, _ := regexp.MatchString(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\] \[查询失败\]`, service.Logs.Text); !matched {
		t.Fatalf("invalid time was not replaced in log: %q", service.Logs.Text)
	}
	if !strings.Contains(service.Logs.Text, "错误: connection failed") {
		t.Fatalf("interface error is missing from log: %q", service.Logs.Text)
	}
}

func TestGetSkuByLinkReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	service := listenService{Area: model.Areas[0]}
	results := make(chan availabilityResult, 1)
	service.getSkuByLink(results, "R001", server.URL)
	result := <-results

	if result.Err == nil || !strings.Contains(result.Err.Error(), "HTTP 502") {
		t.Fatalf("expected HTTP 502 error, got %v", result.Err)
	}
}

func TestGetSkuByLinkParsesRetailPickupResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":{"stores":[{"storeNumber":"R001","partsAvailability":{"SKU/A":{"pickupDisplay":"available"},"SKU/B":{"pickupDisplay":"unavailable"}}}]}}`))
	}))
	defer server.Close()

	service := listenService{Area: model.Areas[0]}
	results := make(chan availabilityResult, 1)
	service.getSkuByLink(results, "R001", server.URL)
	result := <-results

	if result.Err != nil {
		t.Fatalf("unexpected interface error: %v", result.Err)
	}
	if !result.SKUs["R001.SKU/A"] {
		t.Fatal("available SKU was not marked in stock")
	}
	if result.SKUs["R001.SKU/B"] {
		t.Fatal("unavailable SKU was marked in stock")
	}
}

func TestAppleStoreBaseURLForMainlandChina(t *testing.T) {
	service := listenService{Area: model.Areas[0]}
	if got := service.appleStoreBaseURL(); got != "https://www.apple.com.cn" {
		t.Fatalf("unexpected mainland Apple Store base URL: %s", got)
	}
}

func TestRefreshIntervalDefaultsAndCanBeConfigured(t *testing.T) {
	service := listenService{}
	if got := service.GetRefreshIntervalSeconds(); got != DefaultRefreshIntervalSeconds {
		t.Fatalf("expected default refresh interval %d, got %d", DefaultRefreshIntervalSeconds, got)
	}

	service.SetRefreshIntervalSeconds(25)
	if got := service.GetRefreshIntervalSeconds(); got != 25 {
		t.Fatalf("expected configured refresh interval 25, got %d", got)
	}

	service.SetRefreshIntervalSeconds(0)
	if got := service.GetRefreshIntervalSeconds(); got != DefaultRefreshIntervalSeconds {
		t.Fatalf("invalid interval should reset to default, got %d", got)
	}
}
