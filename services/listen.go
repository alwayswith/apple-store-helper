package services

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/golang-module/carbon"
	"github.com/parnurzeal/gorequest"
	"github.com/tidwall/gjson"

	"apple-store-helper/model"
	"apple-store-helper/theme"
	"apple-store-helper/view"
)

const (
	StatusOutStock                = "无货"
	StatusInStock                 = "有货"
	StatusWait                    = "等待"
	StatusError                   = "查询失败"
	DefaultRefreshIntervalSeconds = 15

	Pause   = "暂停"
	Running = "监听中"
)

var Listen = listenService{
	items:  map[string]ListenItem{},
	Status: binding.NewString(),
	Area:   model.Areas[0],
	Logs:   widget.NewLabel(""),
}

type listenService struct {
	items          map[string]ListenItem
	Status         binding.String
	Area           model.Area
	Logs           *widget.Label
	BarkNotifyUrl  string
	refreshSeconds atomic.Int64
}

type ListenItem struct {
	Store        model.Store
	Product      model.Product
	Status       string
	Time         carbon.DateTime
	ErrorMessage string `json:"error_message,omitempty"`
}

type availabilityResult struct {
	StoreNumber string
	SKUs        map[string]bool
	Err         error
}

func (s *listenService) Add(areaTitle string, storeTitle string, productTitle string, barkNotifyUrl string) {
	s.AddStores(areaTitle, []string{storeTitle}, productTitle, barkNotifyUrl)
}

// AddStores adds the same product for every selected store. Existing
// store/product pairs are left untouched, so repeated clicks are safe.
func (s *listenService) AddStores(areaTitle string, storeTitles []string, productTitle string, barkNotifyUrl string) {
	product := Product.GetProduct(areaTitle, productTitle)
	addedAt := currentDateTime()

	for _, storeTitle := range storeTitles {
		store := Store.GetStore(areaTitle, storeTitle)
		uniqKey := store.StoreNumber + "." + product.Code

		if s.items[uniqKey].Store.StoreNumber == "" {
			s.items[uniqKey] = ListenItem{
				Store:   store,
				Product: product,
				Status:  StatusWait,
				Time:    addedAt,
			}
		}
	}

	s.BarkNotifyUrl = barkNotifyUrl
	s.UpdateLogStr()
}

func (s *listenService) Clean() {
	s.items = map[string]ListenItem{}
	s.UpdateLogStr()
}

func (s *listenService) SetListenItems(items map[string]ListenItem) {
	if items == nil {
		items = make(map[string]ListenItem)
	}
	loadedAt := currentDateTime()
	for key, item := range items {
		// Older settings saved an empty DateTime. Carbon keeps that value as
		// invalid (not necessarily zero), which otherwise renders as "".
		if !item.Time.IsValid() {
			item.Time = loadedAt
			items[key] = item
		}
	}
	s.items = items
	s.UpdateLogStr()
}

func (s *listenService) GetListenItems() map[string]ListenItem {
	return s.items
}

func (s *listenService) UpdateLogStr() {
	var str string

	for _, item := range s.items {
		timestamp := item.Time.ToDateTimeString()
		if timestamp == "" {
			timestamp = time.Now().Format("2006-01-02 15:04:05")
		}
		errorSuffix := ""
		if item.ErrorMessage != "" {
			errorSuffix = " 错误: " + strings.ReplaceAll(item.ErrorMessage, "\n", " ")
		}
		str += fmt.Sprintf(
			"[%s] [%s] %s %s%s\n",
			timestamp,
			item.Status,
			item.Store.CityStoreName,
			item.Product.Title,
			errorSuffix,
		)
	}

	s.Logs.SetText(str)
}

func (s *listenService) UpdateStatus(uniqKey string, status string) {
	item := s.items[uniqKey]
	item.Time = currentDateTime()
	item.Status = status
	item.ErrorMessage = ""
	s.items[uniqKey] = item
}

func (s *listenService) UpdateError(uniqKey string, err error) {
	item := s.items[uniqKey]
	item.Time = currentDateTime()
	item.Status = StatusError
	item.ErrorMessage = err.Error()
	s.items[uniqKey] = item
}

func currentDateTime() carbon.DateTime {
	return carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)}
}

func (s *listenService) SetRefreshIntervalSeconds(seconds int) {
	if seconds < 1 {
		seconds = DefaultRefreshIntervalSeconds
	}
	s.refreshSeconds.Store(int64(seconds))
}

func (s *listenService) GetRefreshIntervalSeconds() int {
	seconds := s.refreshSeconds.Load()
	if seconds < 1 {
		return DefaultRefreshIntervalSeconds
	}
	return int(seconds)
}

func (s *listenService) appleStoreBaseURL() string {
	if s.Area.Locale == "zh_CN" {
		return "https://www.apple.com.cn"
	}
	return fmt.Sprintf("https://www.apple.com/%s", strings.Trim(s.Area.ShortCode, "/"))
}

func (s *listenService) Run() {
	s.Status.Set(Pause)

	go func() {
		for {
			nextCheckDelay := 500 * time.Millisecond
			if stats, ok := s.Status.Get(); ok == nil && stats == Running && len(s.items) > 0 {
				skus, storeErrors := s.groupByStore()
				nextCheckDelay = time.Duration(s.GetRefreshIntervalSeconds()) * time.Second

				for key, item := range s.items {
					if err := storeErrors[item.Store.StoreNumber]; err != nil {
						s.UpdateError(key, err)
						continue
					}
					if err := storeErrors["*"]; err != nil {
						s.UpdateError(key, err)
						continue
					}
					status := skus[item.Store.StoreNumber+"."+item.Product.Code]

					if status {
						s.UpdateStatus(key, StatusInStock)
						s.Status.Set(Pause)

						var bagUrl = s.appleStoreBaseURL() + "/shop/bag"
						// 进入购物袋
						s.openBrowser(bagUrl)
						msg := fmt.Sprintf("%s %s 有货", item.Store.CityStoreName, item.Product.Title)
						dialog.ShowInformation("匹配成功", msg, view.Window)
						view.App.SendNotification(&fyne.Notification{
							Title:   "有货提醒",
							Content: msg,
						})
						go s.AlertMp3()
						go s.SendPushNotificationByBark("有货提醒", msg, bagUrl)
						break
					} else {
						s.UpdateStatus(key, StatusOutStock)
					}
				}

				s.UpdateLogStr()
			}

			time.Sleep(nextCheckDelay)
		}
	}()
}

func (s *listenService) groupByStore() (skus map[string]bool, storeErrors map[string]error) {
	skus = map[string]bool{}
	storeErrors = map[string]error{}

	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
			storeErrors["*"] = fmt.Errorf("库存接口处理异常: %v", r)
		}
	}()

	group := map[string][]ListenItem{}
	reqs := map[string]string{}

	for _, item := range s.items {
		group[item.Store.StoreNumber] = append(group[item.Store.StoreNumber], item)
	}

	for storeNumber, items := range group {

		var uri url.URL
		q := uri.Query()
		q.Set("pl", "true")
		q.Set("mts.0", "regular")
		q.Set("store", storeNumber)

		for index, item := range items {
			q.Set("parts."+strconv.FormatInt(int64(index), 10), item.Product.Code)
		}

		queryStr := q.Encode()

		link := fmt.Sprintf("%s/shop/retail/pickup-message?%s", s.appleStoreBaseURL(), queryStr)

		reqs[storeNumber] = link
	}

	count := len(reqs)
	if count < 1 {
		return skus, storeErrors
	}

	ch := make(chan availabilityResult, count)

	for storeNumber, link := range reqs {
		go s.getSkuByLink(ch, storeNumber, link)
	}

	for i := 0; i < count; i++ {
		result := <-ch
		if result.Err != nil {
			storeErrors[result.StoreNumber] = result.Err
			continue
		}
		for key, v := range result.SKUs {
			skus[key] = v
		}
	}

	return skus, storeErrors
}

func (s *listenService) getSkuByLink(ch chan availabilityResult, storeNumber string, skUrl string) {
	skus := map[string]bool{}

	resp, body, errs := gorequest.New().
		Get(skUrl).
		Set("referer", s.appleStoreBaseURL()+"/shop/buy-iphone").
		Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/94.0.4606.71 Safari/537.36").
		Timeout(time.Second * 10).End()
	if len(errs) > 0 {
		log.Println(errs)
		ch <- availabilityResult{StoreNumber: storeNumber, SKUs: skus, Err: fmt.Errorf("请求失败: %v", errs[0])}
		return
	}
	if resp == nil {
		ch <- availabilityResult{StoreNumber: storeNumber, SKUs: skus, Err: fmt.Errorf("接口未返回响应")}
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		ch <- availabilityResult{StoreNumber: storeNumber, SKUs: skus, Err: fmt.Errorf("接口返回 HTTP %d", resp.StatusCode)}
		return
	}
	if !gjson.Valid(body) {
		ch <- availabilityResult{StoreNumber: storeNumber, SKUs: skus, Err: fmt.Errorf("接口返回的数据不是有效 JSON")}
		return
	}

	log.Println(resp.Status, skUrl)
	stores := gjson.Get(body, "body.stores")
	if !stores.Exists() {
		ch <- availabilityResult{StoreNumber: storeNumber, SKUs: skus, Err: fmt.Errorf("接口返回缺少门店库存数据")}
		return
	}
	foundRequestedStore := false
	for _, result := range stores.Array() {
		returnedStoreNumber := result.Get("storeNumber").String()
		if returnedStoreNumber == storeNumber {
			foundRequestedStore = true
		}
		for productCode, availability := range result.Get("partsAvailability").Map() {
			uniqKey := fmt.Sprintf("%s.%s", returnedStoreNumber, productCode)
			skus[uniqKey] = availability.Get("pickupDisplay").String() == "available"
		}
	}
	if !foundRequestedStore {
		ch <- availabilityResult{StoreNumber: storeNumber, SKUs: skus, Err: fmt.Errorf("接口未返回所选门店 %s 的库存数据", storeNumber)}
		return
	}

	ch <- availabilityResult{StoreNumber: storeNumber, SKUs: skus}
}

// 型号对应预约地址
//func (s *listenService) model2Url(productType string) string {
//	// https://www.apple.com.cn/shop/buy-iphone/iphone-16
//	// https://www.apple.com.cn/shop/buy-iphone/iphone-16-pro
//
//	var t string
//	switch productType {
//	case "iphone16promax", "iphone16pro":
//		t = "iphone-16-pro"
//	case "iphone16":
//		t = "iphone-16"
//	}
//
//	return fmt.Sprintf(
//		"https://www.apple.com/%s/shop/buy-iphone/%s",
//		s.Area.ShortCode,
//		t,
//	)
//}

func (s *listenService) openBrowser(link string) {
	parse, err := url.Parse(link)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}

	err = view.App.OpenURL(parse)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
}

func (s *listenService) AlertMp3() {
	reader := bytes.NewReader(theme.Mp3().Content())
	streamer, _, err := mp3.Decode(ioutil.NopCloser(reader))
	if err != nil {
		panic(err)
	}
	defer streamer.Close()

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))
	<-done
}

func (s *listenService) SendPushNotificationByBark(title string, content string, bagUrl string) {

	if len(s.BarkNotifyUrl) <= 0 {
		return
	}

	apiUrl := fmt.Sprintf("%s/%s/%s?url=%s", strings.TrimRight(s.BarkNotifyUrl, "/"), title, content, bagUrl)

	response, err := http.Get(apiUrl)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
}
