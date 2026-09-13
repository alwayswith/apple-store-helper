package main

import (
	"apple-store-helper/common"
	"apple-store-helper/services"
	"apple-store-helper/theme"
	"apple-store-helper/view"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
)

// main 主函数 (Main function)
func main() {
	initMP3Player()
	initFyneApp()

	// 默认地区 (Default Area)
	defaultArea := services.Listen.Area.Title

	// 门店选择器 (Store Selector)
	storeWidget := widget.NewCheckGroup(services.Store.ByAreaTitleForOptions(defaultArea), nil)
	storeScroll := container.NewVScroll(storeWidget)
	storeScroll.SetMinSize(fyne.NewSize(600, 180))

	// 型号选择器 (Product Selector)
	productWidget := widget.NewSelect(services.Product.ByAreaTitleForOptions(defaultArea), nil)
	productWidget.PlaceHolder = "请选择 iPhone 型号"

	// Bark 通知输入框
	barkWidget := widget.NewEntry()
	barkWidget.SetPlaceHolder("https://api.day.app/你的BarkKey")

	// 刷新间隔输入框，单位为秒
	refreshIntervalWidget := widget.NewEntry()
	refreshIntervalWidget.SetText(strconv.Itoa(services.DefaultRefreshIntervalSeconds))
	refreshIntervalWidget.SetPlaceHolder("15")

	// 地区选择器 (Area Selector)
	areaWidget := widget.NewRadioGroup(services.Area.ForOptions(), func(value string) {
		// 防止空值或无效值导致崩溃
		if value == "" {
			return
		}

		storeWidget.Options = services.Store.ByAreaTitleForOptions(value)
		storeWidget.SetSelected(nil)

		productWidget.Options = services.Product.ByAreaTitleForOptions(value)
		productWidget.ClearSelected()

		services.Listen.Area = services.Area.GetArea(value)
		services.Listen.Clean()
	})

	areaWidget.Horizontal = true

	help := `1. 在 Apple 官网将需要购买的型号加入购物车
2. 选择地区、一个或多个门店和型号，点击“添加”按钮，将需要监听的型号批量添加到监听列表
3. 点击“开始”按钮开始监听，检测到有货时会自动打开购物车页面
`

	loadUserSettingsCache(areaWidget, storeWidget, productWidget, barkWidget, refreshIntervalWidget)

	// 初始化 GUI 窗口内容 (Initialize GUI)
	view.Window.SetContent(container.NewVBox(
		widget.NewLabel(help),
		container.New(layout.NewFormLayout(), widget.NewLabel("选择地区:"), areaWidget),
		container.New(layout.NewFormLayout(), widget.NewLabel("选择门店(可多选):"), storeScroll),
		container.New(layout.NewFormLayout(), widget.NewLabel("选择型号:"), productWidget),
		container.New(layout.NewFormLayout(), widget.NewLabel("Bark 通知地址"), barkWidget),
		container.New(layout.NewFormLayout(), widget.NewLabel("刷新间隔(秒):"), refreshIntervalWidget),

		container.NewBorder(nil, nil,
			createActionButtons(areaWidget, storeWidget, productWidget, barkWidget, refreshIntervalWidget),
			createControlButtons(areaWidget, storeWidget, productWidget, barkWidget, refreshIntervalWidget),
		),

		services.Listen.Logs,
		layout.NewSpacer(),
		createVersionLabel(),
	))

	view.Window.Resize(fyne.NewSize(1000, 800))
	view.Window.CenterOnScreen()
	services.Listen.Run()
	view.Window.ShowAndRun()
}

// initMP3Player 初始化 MP3 播放器 (Initialize MP3 player)
func initMP3Player() {
	SampleRate := beep.SampleRate(44100)
	speaker.Init(SampleRate, SampleRate.N(time.Second/10))
}

// initFyneApp 初始化 Fyne 应用 (Initialize Fyne App)
func initFyneApp() {
	view.App = app.NewWithID("apple-store-helper")
	view.App.Settings().SetTheme(&theme.MyTheme{})
	view.Window = view.App.NewWindow("Apple Store Helper")
}

// 加载用户设置缓存 (Load user settings cache)
func loadUserSettingsCache(areaWidget *widget.RadioGroup, storeWidget *widget.CheckGroup, productWidget *widget.Select, barkNotifyWidget *widget.Entry, refreshIntervalWidget *widget.Entry) {
	settings, err := services.LoadSettings()
	if err == nil {
		areaWidget.SetSelected(settings.SelectedArea)
		selectedStores := settings.SelectedStores
		if len(selectedStores) == 0 && settings.SelectedStore != "" {
			selectedStores = []string{settings.SelectedStore}
		}
		storeWidget.SetSelected(selectedStores)
		productWidget.SetSelected(settings.SelectedProduct)
		services.Listen.SetListenItems(settings.ListenItems)
		barkNotifyWidget.SetText(settings.BarkNotifyUrl)
		seconds := settings.RefreshIntervalSeconds
		if seconds < 1 {
			seconds = services.DefaultRefreshIntervalSeconds
		}
		refreshIntervalWidget.SetText(strconv.Itoa(seconds))
		services.Listen.SetRefreshIntervalSeconds(seconds)
	} else {
		areaWidget.SetSelected(services.Listen.Area.Title)
		services.Listen.SetRefreshIntervalSeconds(services.DefaultRefreshIntervalSeconds)
	}
}

func parseRefreshIntervalSeconds(refreshIntervalWidget *widget.Entry) (int, error) {
	seconds, err := strconv.Atoi(strings.TrimSpace(refreshIntervalWidget.Text))
	if err != nil || seconds < 1 || seconds > 3600 {
		return 0, fmt.Errorf("刷新间隔请输入 1 到 3600 之间的整数秒数")
	}
	return seconds, nil
}

func saveCurrentSettings(areaWidget *widget.RadioGroup, storeWidget *widget.CheckGroup, productWidget *widget.Select, barkNotifyWidget *widget.Entry, refreshIntervalSeconds int) error {
	selectedStore := ""
	if len(storeWidget.Selected) > 0 {
		selectedStore = storeWidget.Selected[0]
	}
	return services.SaveSettings(services.UserSettings{
		SelectedArea:           areaWidget.Selected,
		SelectedStore:          selectedStore,
		SelectedStores:         append([]string(nil), storeWidget.Selected...),
		SelectedProduct:        productWidget.Selected,
		BarkNotifyUrl:          barkNotifyWidget.Text,
		RefreshIntervalSeconds: refreshIntervalSeconds,
		ListenItems:            services.Listen.GetListenItems(),
	})
}

// 创建动作按钮 (Create action buttons)
func createActionButtons(areaWidget *widget.RadioGroup, storeWidget *widget.CheckGroup, productWidget *widget.Select, barkNotifyWidget *widget.Entry, refreshIntervalWidget *widget.Entry) *fyne.Container {
	return container.NewHBox(
		widget.NewButton("添加", func() {
			if len(storeWidget.Selected) == 0 || productWidget.Selected == "" {
				dialog.ShowError(errors.New("请至少选择一个门店和型号"), view.Window)
			} else {
				seconds, err := parseRefreshIntervalSeconds(refreshIntervalWidget)
				if err != nil {
					dialog.ShowError(err, view.Window)
					return
				}
				services.Listen.SetRefreshIntervalSeconds(seconds)
				services.Listen.AddStores(areaWidget.Selected, storeWidget.Selected, productWidget.Selected, barkNotifyWidget.Text)
				if err := saveCurrentSettings(areaWidget, storeWidget, productWidget, barkNotifyWidget, seconds); err != nil {
					dialog.ShowError(fmt.Errorf("保存设置失败: %w", err), view.Window)
				}
			}
		}),
		widget.NewButton("清空", func() {
			services.Listen.Clean()
			_ = services.ClearSettings()
			refreshIntervalWidget.SetText(strconv.Itoa(services.DefaultRefreshIntervalSeconds))
			services.Listen.SetRefreshIntervalSeconds(services.DefaultRefreshIntervalSeconds)
		}),
		widget.NewButton("试听(有货提示音)", func() {
			go services.Listen.AlertMp3()
		}),
		widget.NewButton("测试 Bark 通知", func() {
			services.Listen.BarkNotifyUrl = barkNotifyWidget.Text
			services.Listen.SendPushNotificationByBark("有货提醒（测试）", "此为测试提醒，点击通知将跳转到相关链接", "https://www.apple.com.cn/shop/bag")
		}),
	)
}

// 创建控制按钮 (Create control buttons)
func createControlButtons(areaWidget *widget.RadioGroup, storeWidget *widget.CheckGroup, productWidget *widget.Select, barkNotifyWidget *widget.Entry, refreshIntervalWidget *widget.Entry) *fyne.Container {
	return container.NewHBox(
		widget.NewButton("开始", func() {
			seconds, err := parseRefreshIntervalSeconds(refreshIntervalWidget)
			if err != nil {
				dialog.ShowError(err, view.Window)
				return
			}
			services.Listen.SetRefreshIntervalSeconds(seconds)
			if err := saveCurrentSettings(areaWidget, storeWidget, productWidget, barkNotifyWidget, seconds); err != nil {
				dialog.ShowError(fmt.Errorf("保存设置失败: %w", err), view.Window)
				return
			}
			services.Listen.Status.Set(services.Running)
		}),
		widget.NewButton("暂停", func() {
			services.Listen.Status.Set(services.Pause)
		}),
		container.NewCenter(widget.NewLabel("状态:")),
		container.NewCenter(widget.NewLabelWithData(services.Listen.Status)),
	)
}

// createVersionLabel 创建版本标签 (Create version label)
func createVersionLabel() *fyne.Container {
	return container.NewHBox(
		layout.NewSpacer(),
		widget.NewLabel("version: "+common.VERSION),
	)
}
