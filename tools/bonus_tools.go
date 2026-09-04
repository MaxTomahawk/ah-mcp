package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterBonusTools registers the extended Bonus tools after the legacy product tool set.
func RegisterBonusTools(s *server.MCPServer, deps Deps) {
	registerGetBonusOffersNextWeek(s, deps)
	registerGetBonusBox(s, deps)
	// Re-register the existing group tool with period-aware inputs. mcp-go stores
	// tools by name, so this replaces the legacy registration while keeping its name.
	registerGetBonusGroupProductsEnhanced(s, deps)
}

func registerGetBonusOffersNextWeek(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_get_bonus_offers_next_week",
		mcp.WithTitleAnnotation("Albert Heijn: Next Week Bonus Offers"),
		mcp.WithDescription(
			"Get Albert Heijn national Bonus offers for the explicitly published next week period, not the current week. "+
				"Returns top-level valid_from/valid_until plus offers with id, bonus_segment_id, title, original_price, bonus_price, discount_percentage, bonus_mechanism and validity dates. "+
				"If AH has not published a future Bonus period yet, returns a valid empty offers list. "+
				"For group deals with bonus_segment_id, pass the segment plus valid_from/valid_until to ah_get_bonus_group_products.",
		),
		mcp.WithString("limit", mcp.Description("Maximum number of results to return (default 20)")),
		mcp.WithString("query", mcp.Description("Optional keyword filter applied client-side, e.g. 'kaas', 'vlees', 'bier'")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if !deps.IsAuthenticated() {
			return notAuthResult(), nil
		}
		if err := refreshTokens(ctx, deps); err != nil {
			return errResult(fmt.Sprintf("Token refresh failed: %v", err)), nil
		}
		c, err := deps.GetClient()
		if err != nil {
			return errResult(fmt.Sprintf("Client error: %v", err)), nil
		}
		resp, err := buildNextWeekBonusResponse(ctx, c, ahLocalTime(time.Now()), req.GetString("query", ""), req.GetInt("limit", 20))
		if err != nil {
			return errResult(fmt.Sprintf("Failed to get next-week bonus offers: %v", err)), nil
		}
		return jsonResult(resp)
	})
}

func registerGetBonusBox(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_get_bonus_box",
		mcp.WithTitleAnnotation("Albert Heijn: Personal Bonus Box"),
		mcp.WithDescription(
			"Get the current personal Bonus Box offers for the logged-in Albert Heijn account. "+
				"This is account-bound and distinct from ordinary national Bonus offers. "+
				"Returns the active period, maximum_activations when AH provides it, and each offer's identifiers, prices, mechanism, validity and activation_status. "+
				"available reflects ACTIVATABLE and activated reflects ACTIVATED; no selection or activation mutation is performed.",
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if !deps.IsAuthenticated() {
			return notAuthResult(), nil
		}
		if err := refreshTokens(ctx, deps); err != nil {
			return errResult(fmt.Sprintf("Token refresh failed: %v", err)), nil
		}
		c, err := deps.GetClient()
		if err != nil {
			return errResult(fmt.Sprintf("Client error: %v", err)), nil
		}
		resp, err := buildCurrentBonusBoxResponse(ctx, c, ahLocalTime(time.Now()))
		if err != nil {
			return errResult(fmt.Sprintf("Failed to get Bonus Box offers: %v", err)), nil
		}
		return jsonResult(resp)
	})
}

func registerGetBonusGroupProductsEnhanced(s *server.MCPServer, deps Deps) {
	tool := mcp.NewTool("ah_get_bonus_group_products",
		mcp.WithTitleAnnotation("Albert Heijn: Bonus Group Products"),
		mcp.WithDescription(
			"Get all individual products belonging to a specific Albert Heijn bonus promotion group. "+
				"Use this to drill into a deal like '2+1 gratis kaas' or 'Alle yoghurt 25% korting'. "+
				"Get segment_id from the bonus_segment_id field in ah_get_bonus_offers results. "+
				"Returns the same fields as ah_search_products.",
		),
		mcp.WithString("segment_id",
			mcp.Required(),
			mcp.Description("Bonus segment ID from bonus_segment_id in a Bonus offers result"),
		),
		mcp.WithString("valid_from",
			mcp.Description("Optional promotion period start (YYYY-MM-DD). Supply together with valid_until for next-week group offers; omit both for current Bonus."),
		),
		mcp.WithString("valid_until",
			mcp.Description("Optional promotion period end (YYYY-MM-DD). Supply together with valid_from for next-week group offers; omit both for current Bonus."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if !deps.IsAuthenticated() {
			return notAuthResult(), nil
		}
		if err := refreshTokens(ctx, deps); err != nil {
			return errResult(fmt.Sprintf("Token refresh failed: %v", err)), nil
		}
		c, err := deps.GetClient()
		if err != nil {
			return errResult(fmt.Sprintf("Client error: %v", err)), nil
		}

		segmentID := req.GetString("segment_id", "")
		if segmentID == "" {
			return errResult("segment_id is required"), nil
		}

		validFrom := req.GetString("valid_from", "")
		validUntil := req.GetString("valid_until", "")
		if (validFrom == "") != (validUntil == "") {
			return errResult("valid_from and valid_until must be supplied together"), nil
		}
		if validFrom != "" {
			products, err := fetchBonusGroupProductsForPeriod(ctx, c, segmentID, bonusPeriod{Start: validFrom, End: validUntil})
			if err != nil {
				return errResult(fmt.Sprintf("Failed to get bonus group products for %s: %v", segmentID, err)), nil
			}
			return jsonResult(products)
		}

		products, err := c.GetBonusGroupProducts(ctx, segmentID)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to get bonus group products for %s: %v", segmentID, err)), nil
		}

		type item struct {
			ID             int     `json:"id"`
			Title          string  `json:"title"`
			Price          float64 `json:"price"`
			BonusPrice     float64 `json:"bonus_price,omitempty"`
			Unit           string  `json:"unit,omitempty"`
			IsBonus        bool    `json:"is_bonus"`
			BonusMechanism string  `json:"bonus_mechanism,omitempty"`
			ImageURL       string  `json:"image_url,omitempty"`
		}
		results := make([]item, 0, len(products))
		for _, p := range products {
			it := item{
				ID:             p.ID,
				Title:          p.Title,
				IsBonus:        p.IsBonus,
				Unit:           p.UnitSize,
				BonusMechanism: p.BonusMechanism,
			}
			if p.IsBonus {
				it.BonusPrice = p.Price.Now
				it.Price = p.Price.Was
				if it.Price == 0 {
					it.Price = p.Price.Now
				}
			} else {
				it.Price = p.Price.Now
			}
			if len(p.Images) > 0 {
				it.ImageURL = p.Images[0].URL
			}
			results = append(results, it)
		}
		return jsonResult(results)
	})
}
