package tools

import "testing"

func TestShoppingListEntriesIncludeProductAndFreeText(t *testing.T) {
	basket := basketState{
		ItemsInList: []basketItem{{
			ID: 413639,
			Quantity: 2,
			Position: 3,
			Product: basketProduct{ID: 413639, Title: "Test product"},
		}},
		Notes: []basketNote{{
			Description: "TESTITEM CHATGPT",
			Quantity: 1,
			Position: 4,
		}},
	}

	entries := shoppingListEntriesFromBasket(basket)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Name != "Test product" || entries[0].ProductID != 413639 || entries[0].Kind != "product" {
		t.Fatalf("unexpected product entry: %#v", entries[0])
	}
	if entries[1].Name != "TESTITEM CHATGPT" || entries[1].ProductID != 0 || entries[1].Kind != "free_text" {
		t.Fatalf("unexpected free-text entry: %#v", entries[1])
	}
}

func TestBasketHasActiveOrderDistinguishesListOnlyBasket(t *testing.T) {
	listOnly := basketState{ItemsInList: []basketItem{{ID: 1, Quantity: 1}}}
	if basketHasActiveOrder(listOnly) {
		t.Fatal("list-only basket must not be reported as an active online order")
	}

	withOrder := basketState{ItemsInOrder: []basketItem{}, Summary: basketSummary{ShoppingType: "DELIVERY"}}
	if !basketHasActiveOrder(withOrder) {
		t.Fatal("delivery basket must be reported as an active online order")
	}
}

func TestCartViewUsesListItemsWhenNoOnlineOrderExists(t *testing.T) {
	basket := basketState{
		ItemsInList: []basketItem{{
			ID: 2090,
			Quantity: 1,
			Product: basketProduct{ID: 2090, Title: "List product", PriceV2: basketProductPrice{Now: basketMoney{Amount: 1.49}}},
		}},
		Summary: basketSummary{Quantity: 1, Price: basketPrice{TotalPrice: basketMoney{Amount: 1.49}}},
	}

	view := cartViewFromBasket(basket)
	if view.Mode != "shopping_list" || view.HasActiveOrder {
		t.Fatalf("unexpected cart mode: %#v", view)
	}
	if len(view.Items) != 1 || view.Items[0].ProductID != 2090 || view.Items[0].Price != 1.49 {
		t.Fatalf("unexpected cart items: %#v", view.Items)
	}
}

func TestCartViewUsesOrderItemsWhenOnlineOrderExists(t *testing.T) {
	basket := basketState{
		ItemsInList: []basketItem{{ID: 1, Quantity: 1, Product: basketProduct{ID: 1, Title: "List only"}}},
		ItemsInOrder: []basketItem{{ID: 2, Quantity: 3, Product: basketProduct{ID: 2, Title: "Order product", PriceV2: basketProductPrice{Now: basketMoney{Amount: 2.50}}}}},
		Summary: basketSummary{ShoppingType: "DELIVERY", DeliveryDate: "2026-08-27", Quantity: 3, Price: basketPrice{TotalPrice: basketMoney{Amount: 7.50}}},
	}

	view := cartViewFromBasket(basket)
	if view.Mode != "active_order" || !view.HasActiveOrder {
		t.Fatalf("unexpected cart mode: %#v", view)
	}
	if len(view.Items) != 1 || view.Items[0].ProductID != 2 {
		t.Fatalf("cart must expose order items when an order exists: %#v", view.Items)
	}
}

func TestFulfillmentTotalLookupFindsMatchingOrder(t *testing.T) {
	rows := []fulfillmentTotal{{OrderID: 123, TotalPrice: 99.95}, {OrderID: 456, TotalPrice: 12.34}}
	price, ok := fulfillmentTotalForOrder(rows, 456)
	if !ok || price != 12.34 {
		t.Fatalf("fulfillmentTotalForOrder() = %v, %v; want 12.34, true", price, ok)
	}
}
