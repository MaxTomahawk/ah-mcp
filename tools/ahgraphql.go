package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// graphQLDoer is the small slice of the authenticated AH client used by the
// current basket/favourites APIs. *appie.Client satisfies this interface.
type graphQLDoer interface {
	DoGraphQL(ctx context.Context, query string, variables map[string]any, result any) error
}

type basketMoney struct {
	Amount float64 `json:"amount"`
}

type basketProductPrice struct {
	Now basketMoney `json:"now"`
	Was basketMoney `json:"was"`
}

type basketProduct struct {
	ID      int                `json:"id"`
	Title   string             `json:"title"`
	PriceV2 basketProductPrice `json:"priceV2"`
}

type basketItem struct {
	ID              int           `json:"id"`
	Quantity        int           `json:"quantity"`
	AllocatedQuantity int         `json:"allocatedQuantity,omitempty"`
	OriginCode      string        `json:"originCode,omitempty"`
	Position        int           `json:"position,omitempty"`
	IsStrikethrough bool          `json:"isStrikethrough,omitempty"`
	IsClosed        bool          `json:"isClosed,omitempty"`
	Product         basketProduct `json:"product"`
}

type basketNote struct {
	Description     string `json:"description"`
	Quantity        int    `json:"quantity"`
	SearchTerm      string `json:"searchTerm,omitempty"`
	OriginCode      string `json:"originCode,omitempty"`
	Position        int    `json:"position,omitempty"`
	IsStrikethrough bool   `json:"isStrikethrough,omitempty"`
}

type basketPrice struct {
	PriceBeforeDiscount basketMoney `json:"priceBeforeDiscount"`
	PriceAfterDiscount  basketMoney `json:"priceAfterDiscount"`
	TotalPrice          basketMoney `json:"totalPrice"`
	Discount            basketMoney `json:"discount"`
}

type basketSummary struct {
	Price          basketPrice `json:"price"`
	Quantity       int         `json:"quantity"`
	IsCancellable bool        `json:"isCancellable"`
	ShoppingType   string      `json:"shoppingType"`
	DeliveryDate   string      `json:"deliveryDate"`
}

type basketState struct {
	ID             string       `json:"id"`
	ItemsInOrder   []basketItem `json:"itemsInOrder"`
	ItemsInList    []basketItem `json:"itemsInList"`
	ExternalItems  []basketItem `json:"externalItems"`
	Notes          []basketNote `json:"notes"`
	Summary        basketSummary `json:"summary"`
	CanChangeDelivery bool      `json:"canChangeDelivery"`
}

type basketMutationItem struct {
	ID          int     `json:"id,omitempty"`
	Quantity    int     `json:"quantity"`
	Description *string `json:"description,omitempty"`
}

type basketMutationResult struct {
	Status       string      `json:"status"`
	ErrorMessage string      `json:"errorMessage"`
	Result       basketState `json:"result"`
}

type favoriteListItem struct {
	ID        string `json:"id"`
	ProductID int    `json:"productId"`
	Quantity  int    `json:"quantity"`
}

type favoriteList struct {
	ID          string             `json:"id"`
	Description string             `json:"description"`
	TotalSize   int                `json:"totalSize"`
	ImageURL    string             `json:"imageUrl,omitempty"`
	Items       []favoriteListItem `json:"items"`
}

type favoriteProductMutation struct {
	ProductID int `json:"productId"`
	Quantity  int `json:"quantity"`
}

const basketFields = `
    id
    canChangeDelivery
    itemsInList {
      id
      quantity
      originCode
      position
      isStrikethrough
      product {
        id
        title
        priceV2 {
          now { amount }
          was { amount }
        }
      }
    }
    externalItems {
      id
      quantity
      originCode
      position
      isStrikethrough
      product {
        id
        title
        priceV2 {
          now { amount }
          was { amount }
        }
      }
    }
    itemsInOrder {
      id
      quantity
      allocatedQuantity
      originCode
      position
      isClosed
      product {
        id
        title
        priceV2 {
          now { amount }
          was { amount }
        }
      }
    }
    notes {
      description
      quantity
      searchTerm
      originCode
      position
      isStrikethrough
    }
    summary {
      quantity
      isCancellable
      shoppingType
      deliveryDate
      price {
        priceBeforeDiscount { amount }
        priceAfterDiscount { amount }
        totalPrice { amount }
        discount { amount }
      }
    }
`

func fetchBasket(ctx context.Context, c graphQLDoer) (basketState, error) {
	query := `query MCPBasket {
  basket {` + basketFields + `  }
}`
	var response struct {
		Basket basketState `json:"basket"`
	}
	if err := c.DoGraphQL(ctx, query, nil, &response); err != nil {
		return basketState{}, fmt.Errorf("get basket: %w", err)
	}
	return response.Basket, nil
}

func addBasketProducts(ctx context.Context, c graphQLDoer, items []basketMutationItem) (basketState, error) {
	mutation := `mutation MCPBasketItemsAdd($items: [BasketMutation!]!) {
  basketItemsAdd(items: $items) {
    status
    errorMessage
    result {` + basketFields + `    }
  }
}`
	var response struct {
		Mutation basketMutationResult `json:"basketItemsAdd"`
	}
	if err := c.DoGraphQL(ctx, mutation, map[string]any{"items": items}, &response); err != nil {
		return basketState{}, fmt.Errorf("add basket items: %w", err)
	}
	if err := checkMutationStatus("basketItemsAdd", response.Mutation.Status, response.Mutation.ErrorMessage); err != nil {
		return basketState{}, err
	}
	return response.Mutation.Result, nil
}

func updateBasketProducts(ctx context.Context, c graphQLDoer, items []basketMutationItem) (basketState, error) {
	mutation := `mutation MCPBasketItemsUpdate($items: [BasketMutation!]!) {
  basketItemsUpdate(items: $items) {
    status
    errorMessage
    result {` + basketFields + `    }
  }
}`
	var response struct {
		Mutation basketMutationResult `json:"basketItemsUpdate"`
	}
	if err := c.DoGraphQL(ctx, mutation, map[string]any{"items": items}, &response); err != nil {
		return basketState{}, fmt.Errorf("update basket items: %w", err)
	}
	if err := checkMutationStatus("basketItemsUpdate", response.Mutation.Status, response.Mutation.ErrorMessage); err != nil {
		return basketState{}, err
	}
	return response.Mutation.Result, nil
}

func checkMutationStatus(operation, status, message string) error {
	// Some AH GraphQL mutations omit status while still returning a valid result.
	if status == "" || strings.EqualFold(status, "SUCCESS") {
		return nil
	}
	if message == "" {
		message = status
	}
	return fmt.Errorf("%s failed: %s", operation, message)
}

func fetchFavoriteLists(ctx context.Context, c graphQLDoer, ids []string) ([]favoriteList, error) {
	query := `query MCPFavoriteLists($ids: [String!]!) {
  favoriteListV2(ids: $ids) {
    id
    description
    totalSize
    imageUrl
    items {
      id
      productId
      quantity
    }
  }
}`
	if ids == nil {
		ids = []string{}
	}
	for i := range ids {
		ids[i] = strings.ToUpper(ids[i])
	}
	var response struct {
		Lists []favoriteList `json:"favoriteListV2"`
	}
	if err := c.DoGraphQL(ctx, query, map[string]any{"ids": ids}, &response); err != nil {
		return nil, fmt.Errorf("get favourite lists: %w", err)
	}
	return response.Lists, nil
}

func addFavoriteProducts(ctx context.Context, c graphQLDoer, listID string, items []favoriteProductMutation) error {
	mutation := `mutation MCPFavoriteListProductsAdd($id: String!, $products: [FavoriteListProductMutation!]!) {
  favoriteListProductsAddV2(id: $id, products: $products) {
    status
    errorMessage
  }
}`
	var response struct {
		Mutation struct {
			Status       string `json:"status"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"favoriteListProductsAddV2"`
	}
	variables := map[string]any{
		"id":       strings.ToUpper(listID),
		"products": items,
	}
	if err := c.DoGraphQL(ctx, mutation, variables, &response); err != nil {
		return fmt.Errorf("add favourite products: %w", err)
	}
	return checkMutationStatus("favoriteListProductsAddV2", response.Mutation.Status, response.Mutation.ErrorMessage)
}

func deleteFavoriteItems(ctx context.Context, c graphQLDoer, listID string, itemIDs []string) error {
	mutation := `mutation MCPFavoriteListProductsDelete($id: String!, $itemIds: [String!]!) {
  favoriteListProductsDeleteV2(id: $id, itemIds: $itemIds) {
    status
    errorMessage
  }
}`
	var response struct {
		Mutation struct {
			Status       string `json:"status"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"favoriteListProductsDeleteV2"`
	}
	variables := map[string]any{
		"id":      strings.ToUpper(listID),
		"itemIds": itemIDs,
	}
	if err := c.DoGraphQL(ctx, mutation, variables, &response); err != nil {
		return fmt.Errorf("delete favourite products: %w", err)
	}
	return checkMutationStatus("favoriteListProductsDeleteV2", response.Mutation.Status, response.Mutation.ErrorMessage)
}

func favoriteItemIDsForProducts(list favoriteList, productIDs []int) []string {
	wanted := make(map[int]struct{}, len(productIDs))
	for _, id := range productIDs {
		if id > 0 {
			wanted[id] = struct{}{}
		}
	}
	ids := make([]string, 0, len(wanted))
	for _, item := range list.Items {
		if _, ok := wanted[item.ProductID]; ok && item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	// Keep deterministic output even if AH changes list ordering between calls.
	sort.Strings(ids)
	return ids
}

func favoriteListByID(lists []favoriteList, listID string) (favoriteList, bool) {
	for _, list := range lists {
		if strings.EqualFold(list.ID, listID) {
			return list, true
		}
	}
	return favoriteList{}, false
}

func basketHasActiveOrder(b basketState) bool {
	return b.ItemsInOrder != nil || b.Summary.ShoppingType != "" || b.Summary.DeliveryDate != ""
}
