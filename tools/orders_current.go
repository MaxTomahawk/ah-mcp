package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterOrderToolsCurrent preserves the existing working order/receipt tools
// and replaces only order-details/cart handlers that had stale semantics.
func RegisterOrderToolsCurrent(s *server.MCPServer, deps Deps) {
	registerGetOrderHistory(s, deps)
	registerGetPastOrders(s, deps)
	registerGetOrderDetailsCurrent(s, deps)
	registerGetFrequentItems(s, deps)
	registerGetReceipts(s, deps)
	registerGetReceiptDetails(s, deps)
	registerGetCartCurrent(s, deps)
	registerGetCartSummaryCurrent(s, deps)
	registerUpdateCartItemCurrent(s, deps)
	registerRemoveFromCartCurrent(s, deps)
	registerClearCartCurrent(s, deps)
	registerReopenOrder(s, deps)
	registerUpdateOrderItems(s, deps)
	registerRevertOrder(s, deps)
}

type cartItemView struct {
	ProductID int     `json:"product_id"`
	Name      string  `json:"name,omitempty"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price,omitempty"`
}

type cartView struct {
	Mode           string         `json:"mode"`
	HasActiveOrder bool           `json:"has_active_order"`
	BasketID       string         `json:"basket_id,omitempty"`
	Items          []cartItemView `json:"items"`
	TotalItems     int            `json:"total_items"`
	TotalPrice     float64        `json:"total_price"`
	TotalDiscount  float64        `json:"total_discount,omitempty"`
	ShoppingType   string         `json:"shopping_type,omitempty"`
	DeliveryDate   string         `json:"delivery_date,omitempty"`
}

func cartViewFromBasket(b basketState) cartView {
	active := basketHasActiveOrder(b)
	items := b.ItemsInList
	mode := "shopping_list"
	if active {
		mode = "active_order"
		items = b.ItemsInOrder
	}
	result := cartView{
		Mode:           mode,
		HasActiveOrder: active,
		BasketID:       b.ID,
		Items:          make([]cartItemView, 0, len(items)),
		TotalItems:     b.Summary.Quantity,
		TotalPrice:     b.Summary.Price.TotalPrice.Amount,
		TotalDiscount:  b.Summary.Price.Discount.Amount,
		ShoppingType:   b.Summary.ShoppingType,
		DeliveryDate:   b.Summary.DeliveryDate,
	}
	for _, item := range items {
		pid := item.Product.ID
		if pid == 0 {
			pid = item.ID
		}
		result.Items = append(result.Items, cartItemView{
			ProductID: pid,
			Name:      item.Product.Title,
			Quantity:  item.Quantity,
			Price:     item.Product.PriceV2.Now.Amount,
		})
	}
	if result.TotalItems == 0 {
		for _, item := range items {
			result.TotalItems += item.Quantity
		}
	}
	return result
}

func basketListHasProduct(b basketState, productID int) bool {
	for _, item := range b.ItemsInList {
		pid := item.Product.ID
		if pid == 0 {
			pid = item.ID
		}
		if pid == productID {
			return true
		}
	}
	return false
}

type fulfillmentTotal struct {
	OrderID    int
	TotalPrice float64
}

func fulfillmentTotalForOrder(rows []fulfillmentTotal, orderID int) (float64, bool) {
	for _, row := range rows {
		if row.OrderID == orderID && row.TotalPrice > 0 {
			return row.TotalPrice, true
		}
	}
	return 0, false
}

func fetchFulfillmentTotals(ctx context.Context, c graphQLDoer, status string) ([]fulfillmentTotal, error) {
	if status != "OPEN" && status != "CLOSED" {
		return nil, fmt.Errorf("unsupported fulfillment status %q", status)
	}
	query := `query MCPOrderFulfillmentTotals {
  orderFulfillments(status: ` + status + `) {
    result {
      orderId
      totalPrice {
        totalPrice { amount }
      }
    }
  }
}`
	type row struct {
		OrderID    int `json:"orderId"`
		TotalPrice struct {
			TotalPrice struct {
				Amount float64 `json:"amount"`
			} `json:"totalPrice"`
		} `json:"totalPrice"`
	}
	var response struct {
		OrderFulfillments struct {
			Result []row `json:"result"`
		} `json:"orderFulfillments"`
	}
	if err := c.DoGraphQL(ctx, query, nil, &response); err != nil {
		return nil, err
	}
	result := make([]fulfillmentTotal, 0, len(response.OrderFulfillments.Result))
	for _, item := range response.OrderFulfillments.Result {
		result = append(result, fulfillmentTotal{OrderID: item.OrderID, TotalPrice: item.TotalPrice.TotalPrice.Amount})
	}
	return result, nil
}

func lookupOrderTotal(ctx context.Context, c graphQLDoer, orderID int) (float64, bool) {
	for _, status := range []string{"OPEN", "CLOSED"} {
		rows, err := fetchFulfillmentTotals(ctx, c, status)
		if err != nil {
			continue
		}
		if price, ok := fulfillmentTotalForOrder(rows, orderID); ok {
			return price, true
		}
	}
	return 0, false
}

func registerGetOrderDetailsCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_get_order_details",
		mcp.WithTitleAnnotation("Albert Heijn: Order Details"),
		mcp.WithDescription("Get products and totals for a specific AH online order. Corrects missing dependency totals by matching current fulfillment data."),
		mcp.WithString("order_id", mcp.Required(), mcp.Description("Numeric order ID from order history")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		orderID := req.GetInt("order_id", 0)
		if orderID == 0 {
			return errResult("order_id is required"), nil
		}
		order, err := c.GetOrderDetails(ctx, orderID)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to get order details for %d: %v", orderID, err)), nil
		}
		type itemEntry struct {
			ProductID int     `json:"product_id"`
			Name      string  `json:"name,omitempty"`
			Quantity  int     `json:"quantity"`
			Price     float64 `json:"price,omitempty"`
		}
		type orderResult struct {
			ID            string      `json:"id"`
			State         string      `json:"state"`
			Items         []itemEntry `json:"items"`
			TotalPrice    float64     `json:"total_price"`
			TotalDiscount float64     `json:"total_discount,omitempty"`
			TotalSource   string      `json:"total_source,omitempty"`
		}
		items := make([]itemEntry, 0, len(order.Items))
		for _, item := range order.Items {
			entry := itemEntry{ProductID: item.ProductID, Quantity: item.Quantity}
			if item.Product != nil {
				entry.Name = item.Product.Title
				entry.Price = item.Product.Price.Now
			}
			items = append(items, entry)
		}
		total := order.TotalPrice
		source := "order_details"
		if total == 0 {
			if fallback, ok := lookupOrderTotal(ctx, c, orderID); ok {
				total = fallback
				source = "fulfillment"
			}
		}
		return jsonResult(orderResult{ID: order.ID, State: order.State, Items: items, TotalPrice: total, TotalDiscount: order.TotalDiscount, TotalSource: source})
	})
}

func registerGetCartCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_get_cart",
		mcp.WithTitleAnnotation("Albert Heijn: View Cart"),
		mcp.WithDescription("View the current AH basket. Clearly distinguishes a shopping-list-only basket from a real active online order."),
	)
	s.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to get cart/basket: %v", err)), nil
		}
		return jsonResult(cartViewFromBasket(basket))
	})
}

func registerGetCartSummaryCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_get_cart_summary",
		mcp.WithTitleAnnotation("Albert Heijn: Cart Summary"),
		mcp.WithDescription("Get totals for the current AH basket and report whether a real online order exists."),
	)
	s.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to get cart summary: %v", err)), nil
		}
		view := cartViewFromBasket(basket)
		return jsonResult(map[string]any{
			"mode":             view.Mode,
			"has_active_order": view.HasActiveOrder,
			"total_items":      view.TotalItems,
			"total_price":      view.TotalPrice,
			"total_discount":   view.TotalDiscount,
			"shopping_type":    view.ShoppingType,
			"delivery_date":    view.DeliveryDate,
		})
	})
}

func registerUpdateCartItemCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_update_cart_item",
		mcp.WithTitleAnnotation("Albert Heijn: Update Cart Item"),
		mcp.WithDescription("Set product quantity in the AH basket. Without an online order this updates/creates a shopping-list basket item; with an active order it updates that order."),
		mcp.WithString("product_id", mcp.Required(), mcp.Description("Numeric product ID")),
		mcp.WithString("quantity", mcp.Required(), mcp.Description("New quantity; 0 removes")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		productID := req.GetInt("product_id", 0)
		quantity := req.GetInt("quantity", -1)
		if productID <= 0 {
			return errResult("product_id is required"), nil
		}
		if quantity < 0 {
			return errResult("quantity is required and must be >= 0"), nil
		}
		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to read basket: %v", err)), nil
		}
		if basketHasActiveOrder(basket) {
			if _, err := c.GetOrder(ctx); err != nil {
				return errResult(fmt.Sprintf("Failed to load active order: %v", err)), nil
			}
			if err := c.UpdateOrderItem(ctx, productID, quantity); err != nil {
				return errResult(fmt.Sprintf("Failed to update active order item %d: %v", productID, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Product %d quantity set to %d in active online order.", productID, quantity)), nil
		}

		mutation := []basketMutationItem{{ID: productID, Quantity: quantity}}
		if quantity == 0 || basketListHasProduct(basket, productID) {
			if _, err := updateBasketProducts(ctx, c, mutation); err != nil {
				return errResult(fmt.Sprintf("Failed to update shopping-list basket item %d: %v", productID, err)), nil
			}
		} else {
			if _, err := addBasketProducts(ctx, c, mutation); err != nil {
				return errResult(fmt.Sprintf("Failed to add shopping-list basket item %d: %v", productID, err)), nil
			}
		}
		if quantity == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("Product %d removed from shopping-list basket.", productID)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Product %d quantity set to %d in shopping-list basket. No delivery/pickup order has been created.", productID, quantity)), nil
	})
}

func registerRemoveFromCartCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_remove_from_cart",
		mcp.WithTitleAnnotation("Albert Heijn: Remove from Cart"),
		mcp.WithDescription("Remove a product from the current AH basket or active online order."),
		mcp.WithString("product_id", mcp.Required(), mcp.Description("Numeric product ID")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		productID := req.GetInt("product_id", 0)
		if productID <= 0 {
			return errResult("product_id is required"), nil
		}
		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to read basket: %v", err)), nil
		}
		if basketHasActiveOrder(basket) {
			if _, err := c.GetOrder(ctx); err != nil {
				return errResult(fmt.Sprintf("Failed to load active order: %v", err)), nil
			}
			if err := c.RemoveFromOrder(ctx, productID); err != nil {
				return errResult(fmt.Sprintf("Failed to remove product %d from active order: %v", productID, err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Product %d removed from active online order.", productID)), nil
		}
		if !basketListHasProduct(basket, productID) {
			return errResult(fmt.Sprintf("product %d is not present in the shopping-list basket", productID)), nil
		}
		if _, err := updateBasketProducts(ctx, c, []basketMutationItem{{ID: productID, Quantity: 0}}); err != nil {
			return errResult(fmt.Sprintf("Failed to remove product %d from shopping-list basket: %v", productID, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Product %d removed from shopping-list basket.", productID)), nil
	})
}

func registerClearCartCurrent(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_clear_cart",
		mcp.WithTitleAnnotation("Albert Heijn: Clear Cart"),
		mcp.WithDescription("Remove all PRODUCT items from the current AH basket/order. Free-text shopping-list notes are left intact. Requires confirm=\"yes\"."),
		mcp.WithString("confirm", mcp.Required(), mcp.Description("Must be \"yes\"")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if !strings.EqualFold(req.GetString("confirm", ""), "yes") {
			return errResult("confirm must be \"yes\" to clear the cart"), nil
		}
		c, failure := requireCurrentClient(ctx, deps)
		if failure != nil {
			return failure, nil
		}
		basket, err := fetchBasket(ctx, c)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to read basket: %v", err)), nil
		}
		if basketHasActiveOrder(basket) {
			if err := c.ClearOrder(ctx); err != nil {
				return errResult(fmt.Sprintf("Failed to clear active online order: %v", err)), nil
			}
			return mcp.NewToolResultText("Active online order product items cleared."), nil
		}
		if len(basket.ItemsInList) == 0 {
			return mcp.NewToolResultText("Shopping-list basket has no product items to clear."), nil
		}
		items := make([]basketMutationItem, 0, len(basket.ItemsInList))
		for _, item := range basket.ItemsInList {
			pid := item.Product.ID
			if pid == 0 {
				pid = item.ID
			}
			if pid > 0 {
				items = append(items, basketMutationItem{ID: pid, Quantity: 0})
			}
		}
		if _, err := updateBasketProducts(ctx, c, items); err != nil {
			return errResult(fmt.Sprintf("Failed to clear shopping-list basket products: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Cleared %d product item(s) from shopping-list basket.", len(items))), nil
	})
}
