package services

import (
	"testing"

	"apple-store-helper/model"
)

func TestProductsIncludeIPhone18SeriesForEveryArea(t *testing.T) {
	for _, area := range model.Areas {
		products := Area.ProductsByCode(area.Locale)
		families := make(map[string]bool)
		partNumbers := make(map[string]bool)

		for _, product := range products {
			if product.Code == "" {
				t.Fatalf("%s contains a product without a part number", area.Locale)
			}
			if partNumbers[product.Code] {
				t.Fatalf("%s contains duplicate part number %s", area.Locale, product.Code)
			}
			partNumbers[product.Code] = true
			families[product.Type] = true
		}

		for _, family := range []string{"iphone18pro", "iphone18promax"} {
			if !families[family] {
				t.Errorf("%s does not contain %s", area.Locale, family)
			}
		}
	}
}

func TestNewestGenerationIsListedFirst(t *testing.T) {
	products := Area.ProductsByCode("zh_CN")
	if len(products) == 0 || productGeneration(products[0].Type) != 18 {
		t.Fatalf("expected an iPhone 18 model first, got %#v", products)
	}
}
