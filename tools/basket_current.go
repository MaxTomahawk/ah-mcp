package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	appie "github.com/gwillem/appie-go"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const currentShoppingListPath = "/mobile-services/shoppinglist/v2/items?orderBy=userInput&orderByParam=0"

// RegisterBasketToolsCurrent registers the current AH basket/list implementation.
// It preserves the public MCP tool names from RegisterBasketTools while replacing
// obsolete REST list/favourite calls with the current AH basket GraphQL model.
func RegisterBasketToolsCurrent(s *server.MCPServer, deps Deps) {
	registerGetShoppingListCurrent(s, deps)
	registerAddToShoppingListCurrent(s, deps)
	registerAddFreeTextToShoppingListCurrent(s, deps)
	registerRemoveFromShoppingListCurrent(s, deps)
	registerCheckShoppingListItem(s, deps)
	registerClearShoppingListCurrent(s, deps)
	registerShoppingListToOrderCurrent(s, deps)
	registerGetFavoriteListsCurrent(s, deps)
	registerAddToFavoriteListCurrent(s, deps)
	registerRemoveFromFavoriteListCurrent(s, deps)
}

type shoppingListEntry struct {
	Position  int    `json:"position,omitempty"`
	ItemID    string `json:"item_id,omitempty"`
	Name      string `json:"name"`
	ProductID int    `json:"product_id,omitempty"`
	Quantity  int    `json:"quantity"`
	Checked   bool   `json:"checked,omitempty"`
	Kind      string `json:"kind"`
}

func shoppingListEntriesFromBasket(b basketState) []shoppingListEntry {
	entries := make([]shoppingListEntry, 0, len(b.ItemsInList)+len(b.Notes))
	for _, item := range b.ItemsInList {
		pid := item.Product.ID
		if pid == 0 {
			pid = item.ID
		}
		entries = append(entries, shoppingListEntry{
			Position:  item.Position,
			ItemID:    fmt.Sprintf("product:%d", pid),
			Name:      item.Product.Title,
			ProductID: pid,
			Quantity:  item.Quantity,
			Checked:   item.IsStrikethrough,
			Kind:      "product",
		})
	}
	for i, note := range b.Notes {
		entries = append(entries, shoppingListEntry{
			Position: note.Position,
			ItemID:   fmt.Sprintf("note:%d", i),
			Name:     note.Description,
			Quantity: maxInt(note.Quantity, 1),
			Checked:  note.IsStrikethrough,
			Kind:     "free_text",
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		// AH occasionally omits position. Keep response order in that case.
		if entries[i].Position == 0 || entries[j].Position == 0 {
			return false
		}
		return entries[i].Position < entries[j].Position
	})
	return entries
}

func maxInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func requireCurrentClient(ctx context.Context, deps Deps) (*appie.Client, *mcp.CallToolResult) {
	if !deps.IsAuthenticated() {
		return nil, notAuthResult()
	}
	if err := refreshTokens(ctx, deps); err != nil {
		return nil, errResult(fmt.Sprintf("Token refresh failed: %v", err))
	}
	c, err := deps.GetClient()
	if err != nil {
		return nil, errResult(fmt.Sprintf("Client error: %v", err))
	}
	return c, nil
}

func parseProductQuantityItems(raw any) ([]basketMutationItem, error) {
	type inputItem struct {
		ProductID int `json:"product_id"`
		Quantity  int `json:"quantity"`
	}
	var parsed []inputItem
	switch v := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil, fmt.Errorf("items must be a JSON array: %w", err)
		}
	case []any:
		for _, value := range v {
			m, ok := value.(map[string]any)
			if !ok {
				continue
			}
			parsed = append(parsed, inputItem{ProductID: toInt(m["product_id"]), Quantity: toInt(m["quantity"])})
		}
	case nil:
		return nil, fmt.Errorf("items parameter is required")
	default:
		return nil, fmt.Errorf("items must be a JSON array")
	}

	items := make([]basketMutationItem, 0, len(parsed))
	for _, item := range parsed {
		if item.ProductID <= 0 || item.Quantity <= 0 {
			continue
		}
		items = append(items, basketMutationItem{ID: item.ProductID, Quantity: item.Quantity})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no valid items provided (each item needs product_id > 0 and quantity > 0)")
	}
	return items, nil
}

func parseIntArray(raw any, name string) ([]int, error) {
	if raw == nil {
		return nil, nil
	}
	var values []int
	switch v := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &values); err != nil {
			return nil, fmt.Errorf("%s must be a JSON array of integers: %w", name, err)
		}
	case []any:
		for _, item := range v {
			if n := toInt(item); n > 0 {
				values = append(values, n)
			}
		}
	default:
		return nil, fmt.Errorf("%s must be a JSON array of integers", name)
	}
	return values, nil
}

func parseStringArray(raw any, name string) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	var values []string
	switch v := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &values); err != nil {
			return nil, fmt.Errorf("%s must be a JSON array of strings: %w", name, err)
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				values = append(values, s)
			}
		}
	default:
		return nil, fmt.Errorf("%s must be a JSON array of strings", name)
	}
	return values, nil
}

func registerGetShoppingListCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_get_shopping_list",
		mcp.WithTitleAnnotation("Albert Heijn: View Shopping List"),
		mcp.WithDescription("Get the current Albert Heijn basket list, including product items and free-text notes."),
	)
	s.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to get shopping list: %v", err)), nil
		}
		entries := shoppingListEntriesFromBasket(basket)
		if len(entries) == 0 {
			return mcp.NewToolResultText("Your shopping list is empty."), nil
		}
		return jsonResult(entries)
	})
}

func registerAddToShoppingListCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_add_to_shopping_list",
		mcp.WithTitleAnnotation("Albert Heijn: Add to Shopping List"),
		mcp.WithDescription("Add one or more AH products to the current basket/list using the current basket API."),
		mcp.WithString("items", mcp.Required(), mcp.Description(`JSON array: [{"product_id":123456,"quantity":2}]`)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		items, err := parseProductQuantityItems(req.GetArguments()["items"])
		if err != nil {
			return errResult(err.Error()), nil
		}
		basket, err := addBasketProducts(ctx, c, items)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to add items: %v", err)), nil
		}
		mode := "shopping list"
		if basketHasActiveOrder(basket) {
			mode = "active online order"
		}
		return mcp.NewToolResultText(fmt.Sprintf("Added %d product(s) to the AH %s.", len(items), mode)), nil
	})
}

type v2PatchItem struct {
	Description   string `json:"description,omitempty"`
	ProductID     int    `json:"productId,omitempty"`
	Quantity      int    `json:"quantity"`
	Type          string `json:"type"`
	OriginCode    string `json:"originCode"`
	SearchTerm    string `json:"searchTerm,omitempty"`
	StrikeThrough bool   `json:"strikeThrough"`
}

func patchV2ShoppingList(ctx context.Context, c *appie.Client, items []v2PatchItem) error {
	if len(items) == 0 {
		return nil
	}
	var response any
	if err := c.DoRequest(ctx, http.MethodPatch, currentShoppingListPath, map[string]any{"items": items}, &response); err != nil {
		return err
	}
	return nil
}

func registerAddFreeTextToShoppingListCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_add_free_text_to_shopping_list",
		mcp.WithTitleAnnotation("Albert Heijn: Add Free-Text to Shopping List"),
		mcp.WithDescription("Add a free-text note to the AH shopping list."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Free-text item description")),
		mcp.WithString("quantity", mcp.Description("Quantity (default 1)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		name := strings.TrimSpace(req.GetString("name", ""))
		if name == "" {
			return errResult("name is required"), nil
		}
		quantity := req.GetInt("quantity", 1)
		if quantity < 1 {
			quantity = 1
		}
		items := []v2PatchItem{{Description: name, Quantity: quantity, Type: "SHOPPABLE", OriginCode: "PRD", SearchTerm: name}}
		if err := patchV2ShoppingList(ctx, c, items); err != nil {
			return errResult(fmt.Sprintf("Failed to add free-text item: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Added '%s' (x%d) to shopping list.", name, quantity)), nil
	})
}

func registerRemoveFromShoppingListCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_remove_from_shopping_list",
		mcp.WithTitleAnnotation("Albert Heijn: Remove from Shopping List"),
		mcp.WithDescription("Remove product items by product_ids and/or free-text notes by names from the AH shopping list."),
		mcp.WithString("product_ids", mcp.Description("JSON array of product IDs")),
		mcp.WithString("names", mcp.Description("JSON array of free-text names")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		productIDs, err := parseIntArray(req.GetArguments()["product_ids"], "product_ids")
		if err != nil {
			return errResult(err.Error()), nil
		}
		names, err := parseStringArray(req.GetArguments()["names"], "names")
		if err != nil {
			return errResult(err.Error()), nil
		}
		if len(productIDs) == 0 && len(names) == 0 {
			return errResult("provide at least one product_id or name to remove"), nil
		}

		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to read shopping list: %v", err)), nil
		}
		productsPresent := map[int]struct{}{}
		for _, item := range basket.ItemsInList {
			pid := item.Product.ID
			if pid == 0 {
				pid = item.ID
			}
			productsPresent[pid] = struct{}{}
		}
		var productMutations []basketMutationItem
		for _, pid := range productIDs {
			if _, ok := productsPresent[pid]; ok {
				productMutations = append(productMutations, basketMutationItem{ID: pid, Quantity: 0})
			}
		}
		if len(productMutations) > 0 {
			if _, err := updateBasketProducts(ctx, c, productMutations); err != nil {
				return errResult(fmt.Sprintf("Failed to remove product item(s): %v", err)), nil
			}
		}

		wantedNames := map[string]struct{}{}
		for _, name := range names {
			wantedNames[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
		}
		var noteMutations []v2PatchItem
		for _, note := range basket.Notes {
			if _, ok := wantedNames[strings.ToLower(strings.TrimSpace(note.Description))]; ok {
				noteMutations = append(noteMutations, v2PatchItem{Description: note.Description, Quantity: 0, Type: "SHOPPABLE", OriginCode: "PRD"})
			}
		}
		if len(noteMutations) > 0 {
			if err := patchV2ShoppingList(ctx, c, noteMutations); err != nil {
				return errResult(fmt.Sprintf("Failed to remove free-text item(s): %v", err)), nil
			}
		}
		removed := len(productMutations) + len(noteMutations)
		if removed == 0 {
			return errResult("no matching items found on the shopping list"), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Removed %d matching item(s) from shopping list.", removed)), nil
	})
}

func registerClearShoppingListCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_clear_shopping_list",
		mcp.WithTitleAnnotation("Albert Heijn: Clear Shopping List"),
		mcp.WithDescription("Remove ALL product items and free-text notes from the AH shopping list. Does not clear products in an active online order. Requires confirm=\"yes\"."),
		mcp.WithString("confirm", mcp.Required(), mcp.Description("Must be \"yes\"")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if req.GetString("confirm", "") != "yes" {
			return errResult("confirm must be \"yes\" to clear the shopping list"), nil
		}
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to read shopping list: %v", err)), nil
		}
		if len(basket.ItemsInList) == 0 && len(basket.Notes) == 0 {
			return mcp.NewToolResultText("Shopping list is already empty."), nil
		}
		productMutations := make([]basketMutationItem, 0, len(basket.ItemsInList))
		for _, item := range basket.ItemsInList {
			pid := item.Product.ID
			if pid == 0 {
				pid = item.ID
			}
			if pid > 0 {
				productMutations = append(productMutations, basketMutationItem{ID: pid, Quantity: 0})
			}
		}
		if len(productMutations) > 0 {
			if _, err := updateBasketProducts(ctx, c, productMutations); err != nil {
				return errResult(fmt.Sprintf("Failed to clear product items: %v", err)), nil
			}
		}
		noteMutations := make([]v2PatchItem, 0, len(basket.Notes))
		for _, note := range basket.Notes {
			if note.Description != "" {
				noteMutations = append(noteMutations, v2PatchItem{Description: note.Description, Quantity: 0, Type: "SHOPPABLE", OriginCode: "PRD"})
			}
		}
		if err := patchV2ShoppingList(ctx, c, noteMutations); err != nil {
			return errResult(fmt.Sprintf("Failed to clear free-text items: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Shopping list cleared (%d product item(s), %d free-text item(s)).", len(productMutations), len(noteMutations))), nil
	})
}

func registerShoppingListToOrderCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_shopping_list_to_order",
		mcp.WithTitleAnnotation("Albert Heijn: Add List Products to Active Order"),
		mcp.WithDescription("Add unchecked product items from the AH shopping list to an EXISTING active online order. This tool does not create a delivery/pickup order or reserve a slot."),
	)
	s.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to read basket: %v", err)), nil
		}
		if !basketHasActiveOrder(basket) {
			return errResult("No active online order exists. Your products are in the AH shopping-list basket. Choose delivery/pickup and a slot in AH first; this MCP does not guess or create checkout reservations."), nil
		}
		var items []appie.OrderItem
		for _, item := range basket.ItemsInList {
			if item.IsStrikethrough {
				continue
			}
			pid := item.Product.ID
			if pid == 0 {
				pid = item.ID
			}
			if pid > 0 && item.Quantity > 0 {
				items = append(items, appie.OrderItem{ProductID: pid, Quantity: item.Quantity})
			}
		}
		if len(items) == 0 {
			return mcp.NewToolResultText("No unchecked product items are available to add to the active order."), nil
		}
		// Populate the active-order headers before using the existing, verified order write path.
		if _, err := c.GetOrder(ctx); err != nil {
			return errResult(fmt.Sprintf("Failed to load active order: %v", err)), nil
		}
		if err := c.AddToOrder(ctx, items); err != nil {
			return errResult(fmt.Sprintf("Failed to add shopping-list products to active order: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Added %d shopping-list product(s) to the active online order. The source list is left intact for safety.", len(items))), nil
	})
}

func registerGetFavoriteListsCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_get_favorite_lists",
		mcp.WithTitleAnnotation("Albert Heijn: View Favourite Lists"),
		mcp.WithDescription("List all AH saved/favourite lists via the current favoriteListV2 GraphQL API."),
	)
	s.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		lists, err := fetchFavoriteLists(ctx, c, nil)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to get favorite lists: %v", err)), nil
		}
		type entry struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			ItemCount int    `json:"item_count"`
		}
		result := make([]entry, 0, len(lists))
		for _, list := range lists {
			result = append(result, entry{ID: list.ID, Name: list.Description, ItemCount: list.TotalSize})
		}
		return jsonResult(result)
	})
}

func registerAddToFavoriteListCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_add_to_favorite_list",
		mcp.WithTitleAnnotation("Albert Heijn: Add to Favourite List"),
		mcp.WithDescription("Add products to an AH favourite list. Get list_id from ah_get_favorite_lists."),
		mcp.WithString("list_id", mcp.Required(), mcp.Description("Favourite list ID")),
		mcp.WithString("items", mcp.Required(), mcp.Description(`JSON array: [{"product_id":123456,"quantity":1}]`)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		listID := strings.TrimSpace(req.GetString("list_id", ""))
		if listID == "" {
			return errResult("list_id is required"), nil
		}
		items, err := parseProductQuantityItems(req.GetArguments()["items"])
		if err != nil {
			return errResult(err.Error()), nil
		}
		products := make([]favoriteProductMutation, 0, len(items))
		for _, item := range items {
			products = append(products, favoriteProductMutation{ProductID: item.ID, Quantity: item.Quantity})
		}
		if err := addFavoriteProducts(ctx, c, listID, products); err != nil {
			return errResult(fmt.Sprintf("Failed to add to favorite list: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Added %d product(s) to favorite list %s.", len(products), listID)), nil
	})
}

func registerRemoveFromFavoriteListCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_remove_from_favorite_list",
		mcp.WithTitleAnnotation("Albert Heijn: Remove from Favourite List"),
		mcp.WithDescription("Remove products from an AH favourite list. Product IDs are resolved to AH favourite-item UUIDs before deletion."),
		mcp.WithString("list_id", mcp.Required(), mcp.Description("Favourite list ID")),
		mcp.WithString("product_ids", mcp.Required(), mcp.Description("JSON array of product IDs")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		listID := strings.TrimSpace(req.GetString("list_id", ""))
		if listID == "" {
			return errResult("list_id is required"), nil
		}
		productIDs, err := parseIntArray(req.GetArguments()["product_ids"], "product_ids")
		if err != nil {
			return errResult(err.Error()), nil
		}
		if len(productIDs) == 0 {
			return errResult("no valid product_ids provided"), nil
		}
		lists, err := fetchFavoriteLists(ctx, c, []string{listID})
		if err != nil {
			return errResult(fmt.Sprintf("Failed to read favorite list: %v", err)), nil
		}
		list, ok := favoriteListByID(lists, listID)
		if !ok {
			return errResult(fmt.Sprintf("favorite list %s not found", listID)), nil
		}
		itemIDs := favoriteItemIDsForProducts(list, productIDs)
		if len(itemIDs) == 0 {
			return errResult("none of the specified products found in favorite list"), nil
		}
		if err := deleteFavoriteItems(ctx, c, listID, itemIDs); err != nil {
			return errResult(fmt.Sprintf("Failed to remove from favorite list: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Removed %d matching product(s) from favorite list %s.", len(itemIDs), listID)), nil
	})
}
