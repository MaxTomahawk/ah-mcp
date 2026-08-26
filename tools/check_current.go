package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func parseShoppingListItemRef(ref string) (kind string, productID int, noteIndex int, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", 0, -1, fmt.Errorf("item_id is required")
	}
	if strings.HasPrefix(ref, "product:") {
		id, convErr := strconv.Atoi(strings.TrimPrefix(ref, "product:"))
		if convErr != nil || id <= 0 {
			return "", 0, -1, fmt.Errorf("invalid product item_id %q", ref)
		}
		return "product", id, -1, nil
	}
	if strings.HasPrefix(ref, "note:") {
		index, convErr := strconv.Atoi(strings.TrimPrefix(ref, "note:"))
		if convErr != nil || index < 0 {
			return "", 0, -1, fmt.Errorf("invalid free-text item_id %q", ref)
		}
		return "note", 0, index, nil
	}
	// Backward-compatible numeric IDs are interpreted as product IDs.
	if id, convErr := strconv.Atoi(ref); convErr == nil && id > 0 {
		return "product", id, -1, nil
	}
	return "", 0, -1, fmt.Errorf("unsupported item_id %q; use an item_id returned by ah_get_shopping_list", ref)
}

func checkedBasketMutation(productID, quantity int, checked bool) basketMutationItem {
	if quantity < 1 {
		quantity = 1
	}
	return basketMutationItem{ID: productID, Quantity: quantity, IsStrikethrough: &checked}
}

func registerCheckShoppingListItemCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_check_shopping_list_item",
		mcp.WithTitleAnnotation("Albert Heijn: Check Shopping List Item"),
		mcp.WithDescription("Mark a product or free-text item on the main AH shopping list as checked/unchecked. Use item_id from ah_get_shopping_list."),
		mcp.WithString("item_id", mcp.Required(), mcp.Description("Item ID returned by ah_get_shopping_list, e.g. product:413639 or note:0")),
		mcp.WithString("checked", mcp.Description("true to check, false to uncheck (default true)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		kind, productID, noteIndex, err := parseShoppingListItemRef(req.GetString("item_id", ""))
		if err != nil {
			return errResult(err.Error()), nil
		}
		checked := true
		if raw, ok := req.GetArguments()["checked"]; ok {
			switch v := raw.(type) {
			case bool:
				checked = v
			case string:
				parsed, parseErr := strconv.ParseBool(v)
				if parseErr != nil {
					return errResult("checked must be true or false"), nil
				}
				checked = parsed
			}
		}

		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to read shopping list: %v", err)), nil
		}

		switch kind {
		case "product":
			quantity := 0
			for _, item := range basket.ItemsInList {
				pid := item.Product.ID
				if pid == 0 {
					pid = item.ID
				}
				if pid == productID {
					quantity = item.Quantity
					break
				}
			}
			if quantity == 0 {
				return errResult(fmt.Sprintf("product %d is not present on the main shopping list", productID)), nil
			}
			if _, err := updateBasketProducts(ctx, c, []basketMutationItem{checkedBasketMutation(productID, quantity, checked)}); err != nil {
				return errResult(fmt.Sprintf("Failed to change checked state: %v", err)), nil
			}
		case "note":
			if noteIndex < 0 || noteIndex >= len(basket.Notes) {
				return errResult("free-text item no longer exists; refresh the shopping list and use its current item_id"), nil
			}
			note := basket.Notes[noteIndex]
			if err := patchV2ShoppingList(ctx, c, []v2PatchItem{{
				Description:   note.Description,
				Quantity:      maxInt(note.Quantity, 1),
				Type:          "SHOPPABLE",
				OriginCode:    "PRD",
				SearchTerm:    note.SearchTerm,
				StrikeThrough: checked,
			}}); err != nil {
				return errResult(fmt.Sprintf("Failed to change free-text checked state: %v", err)), nil
			}
		}

		state := "unchecked"
		if checked {
			state = "checked"
		}
		return mcp.NewToolResultText(fmt.Sprintf("Shopping-list item marked %s.", state)), nil
	})
}

// RegisterBasketToolsModern is the final current basket tool set. It replaces
// the last obsolete v3 checked-item endpoint as well as the list/favourite APIs.
func RegisterBasketToolsModern(s *server.MCPServer, deps Deps) {
	registerGetShoppingListCurrent(s, deps)
	registerAddToShoppingListCurrent(s, deps)
	registerAddFreeTextToShoppingListCurrent(s, deps)
	registerRemoveFromShoppingListCurrent(s, deps)
	registerCheckShoppingListItemCurrent(s, deps)
	registerClearShoppingListCurrent(s, deps)
	registerShoppingListToOrderCurrent(s, deps)
	registerGetFavoriteListsCurrent(s, deps)
	registerAddToFavoriteListCurrent(s, deps)
	registerRemoveFromFavoriteListCurrent(s, deps)
}
