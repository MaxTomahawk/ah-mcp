package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeGraphQLCall struct {
	query     string
	variables map[string]any
}

type fakeGraphQLClient struct {
	responses []any
	calls     []fakeGraphQLCall
}

func (f *fakeGraphQLClient) DoGraphQL(_ context.Context, query string, variables map[string]any, result any) error {
	f.calls = append(f.calls, fakeGraphQLCall{query: query, variables: variables})
	if len(f.responses) == 0 {
		return nil
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}

func TestFetchBasketDecodesListOrderNotesAndSummary(t *testing.T) {
	client := &fakeGraphQLClient{responses: []any{map[string]any{
		"basket": map[string]any{
			"id": "basket-1",
			"itemsInList": []any{map[string]any{
				"id": 589886,
				"quantity": 2,
				"originCode": "PRD",
				"product": map[string]any{
					"id": 589886,
					"title": "Beemster Royaal belegen plakken",
					"priceV2": map[string]any{"now": map[string]any{"amount": 2.99}},
				},
			}},
			"itemsInOrder": []any{map[string]any{
				"id": 137739,
				"quantity": 1,
				"originCode": "PRD",
				"product": map[string]any{
					"id": 137739,
					"title": "Coca-Cola zero sugar",
					"priceV2": map[string]any{"now": map[string]any{"amount": 2.79}},
				},
			}},
			"notes": []any{map[string]any{
				"description": "verse bloemen",
				"quantity": 1,
				"searchTerm": "verse bloemen",
				"originCode": "PRD",
			}},
			"summary": map[string]any{
				"quantity": 3,
				"shoppingType": "DELIVERY",
				"deliveryDate": "2026-08-27",
				"price": map[string]any{
					"totalPrice": map[string]any{"amount": 8.77},
					"discount": map[string]any{"amount": 1.00},
				},
			},
		},
	}}}

	basket, err := fetchBasket(context.Background(), client)
	if err != nil {
		t.Fatalf("fetchBasket() error = %v", err)
	}
	if basket.ID != "basket-1" {
		t.Fatalf("basket.ID = %q, want basket-1", basket.ID)
	}
	if len(basket.ItemsInList) != 1 || basket.ItemsInList[0].Product.Title != "Beemster Royaal belegen plakken" {
		t.Fatalf("unexpected itemsInList: %#v", basket.ItemsInList)
	}
	if len(basket.ItemsInOrder) != 1 || basket.ItemsInOrder[0].Product.ID != 137739 {
		t.Fatalf("unexpected itemsInOrder: %#v", basket.ItemsInOrder)
	}
	if len(basket.Notes) != 1 || basket.Notes[0].Description != "verse bloemen" {
		t.Fatalf("unexpected notes: %#v", basket.Notes)
	}
	if basket.Summary.Price.TotalPrice.Amount != 8.77 {
		t.Fatalf("total price = %v, want 8.77", basket.Summary.Price.TotalPrice.Amount)
	}
	if len(client.calls) != 1 || !strings.Contains(client.calls[0].query, "basket") {
		t.Fatalf("expected one basket GraphQL call, got %#v", client.calls)
	}
}

func TestAddBasketProductsUsesBasketItemsAdd(t *testing.T) {
	client := &fakeGraphQLClient{responses: []any{map[string]any{
		"basketItemsAdd": map[string]any{
			"status": "SUCCESS",
			"result": map[string]any{
				"itemsInList": []any{map[string]any{"id": 2090, "quantity": 1}},
			},
		},
	}}}

	basket, err := addBasketProducts(context.Background(), client, []basketMutationItem{{ID: 2090, Quantity: 1}})
	if err != nil {
		t.Fatalf("addBasketProducts() error = %v", err)
	}
	if len(basket.ItemsInList) != 1 || basket.ItemsInList[0].ID != 2090 {
		t.Fatalf("unexpected basket result: %#v", basket)
	}
	if len(client.calls) != 1 || !strings.Contains(client.calls[0].query, "basketItemsAdd") {
		t.Fatalf("expected basketItemsAdd mutation, got %#v", client.calls)
	}
	items, ok := client.calls[0].variables["items"].([]basketMutationItem)
	if !ok || len(items) != 1 || items[0].ID != 2090 || items[0].Quantity != 1 {
		t.Fatalf("unexpected mutation variables: %#v", client.calls[0].variables)
	}
}

func TestUpdateBasketProductsUsesQuantityZeroForRemoval(t *testing.T) {
	client := &fakeGraphQLClient{responses: []any{map[string]any{
		"basketItemsUpdate": map[string]any{
			"status": "SUCCESS",
			"result": map[string]any{"itemsInList": []any{}},
		},
	}}}

	_, err := updateBasketProducts(context.Background(), client, []basketMutationItem{{ID: 413639, Quantity: 0}})
	if err != nil {
		t.Fatalf("updateBasketProducts() error = %v", err)
	}
	if len(client.calls) != 1 || !strings.Contains(client.calls[0].query, "basketItemsUpdate") {
		t.Fatalf("expected basketItemsUpdate mutation, got %#v", client.calls)
	}
	items, ok := client.calls[0].variables["items"].([]basketMutationItem)
	if !ok || len(items) != 1 || items[0].Quantity != 0 {
		t.Fatalf("removal must use quantity 0, got %#v", client.calls[0].variables)
	}
}

func TestFetchFavoriteListsUsesFavoriteListV2WithEmptyIDs(t *testing.T) {
	client := &fakeGraphQLClient{responses: []any{map[string]any{
		"favoriteListV2": []any{map[string]any{
			"id": "list-1",
			"description": "Mijn favorieten",
			"totalSize": 1,
			"items": []any{map[string]any{
				"id": "item-uuid-1",
				"productId": 572638,
				"quantity": 1,
			}},
		}},
	}}}

	lists, err := fetchFavoriteLists(context.Background(), client, nil)
	if err != nil {
		t.Fatalf("fetchFavoriteLists() error = %v", err)
	}
	if len(lists) != 1 || lists[0].Description != "Mijn favorieten" || lists[0].Items[0].ID != "item-uuid-1" {
		t.Fatalf("unexpected lists: %#v", lists)
	}
	ids, ok := client.calls[0].variables["ids"].([]string)
	if !ok || len(ids) != 0 {
		t.Fatalf("expected ids: [] for all favourite lists, got %#v", client.calls[0].variables["ids"])
	}
}

func TestFavoriteItemIDsForProductsMapsProductIDsToItemUUIDs(t *testing.T) {
	list := favoriteList{
		Items: []favoriteListItem{
			{ID: "uuid-a", ProductID: 100, Quantity: 1},
			{ID: "uuid-b", ProductID: 200, Quantity: 2},
		},
	}

	ids := favoriteItemIDsForProducts(list, []int{200, 999, 100})
	if len(ids) != 2 || ids[0] != "uuid-a" || ids[1] != "uuid-b" {
		t.Fatalf("favoriteItemIDsForProducts() = %#v, want [uuid-a uuid-b]", ids)
	}
}
