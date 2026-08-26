package tools

import "testing"

func TestParseShoppingListItemRef(t *testing.T) {
	kind, productID, noteIndex, err := parseShoppingListItemRef("product:413639")
	if err != nil || kind != "product" || productID != 413639 || noteIndex != -1 {
		t.Fatalf("product ref parsed as kind=%q product=%d note=%d err=%v", kind, productID, noteIndex, err)
	}

	kind, productID, noteIndex, err = parseShoppingListItemRef("note:2")
	if err != nil || kind != "note" || productID != 0 || noteIndex != 2 {
		t.Fatalf("note ref parsed as kind=%q product=%d note=%d err=%v", kind, productID, noteIndex, err)
	}

	kind, productID, noteIndex, err = parseShoppingListItemRef("413639")
	if err != nil || kind != "product" || productID != 413639 || noteIndex != -1 {
		t.Fatalf("numeric compatibility ref parsed as kind=%q product=%d note=%d err=%v", kind, productID, noteIndex, err)
	}
}

func TestCheckedBasketMutationPreservesQuantity(t *testing.T) {
	checked := true
	item := checkedBasketMutation(413639, 3, checked)
	if item.ID != 413639 || item.Quantity != 3 || item.IsStrikethrough == nil || !*item.IsStrikethrough {
		t.Fatalf("unexpected checked mutation: %#v", item)
	}

	checked = false
	item = checkedBasketMutation(413639, 3, checked)
	if item.IsStrikethrough == nil || *item.IsStrikethrough {
		t.Fatalf("unchecked mutation must explicitly send false: %#v", item)
	}
}
