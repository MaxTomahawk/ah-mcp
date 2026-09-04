package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

var ahAmsterdamLocation = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		panic(fmt.Sprintf("load Europe/Amsterdam timezone: %v", err))
	}
	return loc
}()

func ahLocalTime(now time.Time) time.Time {
	return now.In(ahAmsterdamLocation)
}

type bonusAPIDoer interface {
	DoRequest(ctx context.Context, method, path string, body, result any) error
	DoGraphQL(ctx context.Context, query string, variables map[string]any, result any) error
}

type bonusPeriod struct {
	Start      string
	End        string
	Categories []string
}

type bonusOffer struct {
	ID                 int     `json:"id,omitempty"`
	BonusSegmentID     string  `json:"bonus_segment_id,omitempty"`
	Title              string  `json:"title"`
	OriginalPrice      float64 `json:"original_price,omitempty"`
	BonusPrice         float64 `json:"bonus_price"`
	DiscountPercentage float64 `json:"discount_percentage,omitempty"`
	BonusMechanism     string  `json:"bonus_mechanism,omitempty"`
	ValidFrom          string  `json:"valid_from,omitempty"`
	ValidUntil         string  `json:"valid_until,omitempty"`
}

type bonusBoxOffer struct {
	ID                 string  `json:"id,omitempty"`
	BonusSegmentID     string  `json:"bonus_segment_id,omitempty"`
	HQID               int     `json:"hq_id,omitempty"`
	Title              string  `json:"title"`
	OriginalPrice      float64 `json:"original_price,omitempty"`
	BonusPrice         float64 `json:"bonus_price,omitempty"`
	DiscountPercentage float64 `json:"discount_percentage,omitempty"`
	BonusMechanism     string  `json:"bonus_mechanism,omitempty"`
	ValidFrom          string  `json:"valid_from,omitempty"`
	ValidUntil         string  `json:"valid_until,omitempty"`
	ActivationStatus   string  `json:"activation_status,omitempty"`
	Available          bool    `json:"available"`
	Activated          bool    `json:"activated"`
}

type bonusBoxResponse struct {
	ValidFrom          string          `json:"valid_from,omitempty"`
	ValidUntil         string          `json:"valid_until,omitempty"`
	MaximumActivations int             `json:"maximum_activations,omitempty"`
	Offers             []bonusBoxOffer `json:"offers"`
}

func fetchBonusPeriods(ctx context.Context, c bonusAPIDoer) ([]bonusPeriod, error) {
	var response struct {
		Periods []struct {
			Start string `json:"bonusStartDate"`
			End   string `json:"bonusEndDate"`
			Tabs  []struct {
				Metadata []struct {
					BonusType   string `json:"bonusType"`
					Description string `json:"description"`
				} `json:"urlMetadataList"`
			} `json:"tabs"`
		} `json:"periods"`
	}
	if err := c.DoRequest(ctx, http.MethodGet, "/mobile-services/bonuspage/v3/metadata", nil, &response); err != nil {
		return nil, fmt.Errorf("get bonus metadata: %w", err)
	}

	periods := make([]bonusPeriod, 0, len(response.Periods))
	for _, raw := range response.Periods {
		p := bonusPeriod{Start: raw.Start, End: raw.End}
		seen := make(map[string]bool)
		for _, tab := range raw.Tabs {
			for _, meta := range tab.Metadata {
				if meta.BonusType == "NATIONAL" && meta.Description != "" && !seen[meta.Description] {
					seen[meta.Description] = true
					p.Categories = append(p.Categories, meta.Description)
				}
			}
		}
		periods = append(periods, p)
	}
	return periods, nil
}

func selectCurrentBonusPeriod(periods []bonusPeriod, now time.Time) (bonusPeriod, bool) {
	today := now.Format("2006-01-02")
	for _, p := range periods {
		if p.Start <= today && today <= p.End {
			return p, true
		}
	}
	return bonusPeriod{}, false
}

func selectNextBonusPeriod(periods []bonusPeriod, now time.Time) (bonusPeriod, bool) {
	today := now.Format("2006-01-02")
	future := make([]bonusPeriod, 0, len(periods))
	for _, p := range periods {
		if p.Start > today {
			future = append(future, p)
		}
	}
	if len(future) == 0 {
		return bonusPeriod{}, false
	}
	sort.Slice(future, func(i, j int) bool { return future[i].Start < future[j].Start })
	return future[0], true
}

func fetchBonusOffersForPeriod(ctx context.Context, c bonusAPIDoer, period bonusPeriod) ([]bonusOffer, error) {
	type product struct {
		ID             int     `json:"webshopId"`
		Title          string  `json:"title"`
		PriceBefore    float64 `json:"priceBeforeBonus"`
		CurrentPrice   float64 `json:"currentPrice"`
		BonusMechanism string  `json:"bonusMechanism"`
	}
	type group struct {
		ID                  string  `json:"id"`
		Title               string  `json:"segmentDescription"`
		DiscountDescription string  `json:"discountDescription"`
		ExampleFromPrice    float64 `json:"exampleFromPrice"`
		ExampleForPrice     float64 `json:"exampleForPrice"`
	}
	type section struct {
		Items []struct {
			Product *product `json:"product,omitempty"`
			Group   *group   `json:"bonusGroup,omitempty"`
		} `json:"bonusGroupOrProducts"`
	}

	var offers []bonusOffer
	seen := make(map[string]bool)
	for _, category := range period.Categories {
		params := url.Values{}
		params.Set("application", "AHWEBSHOP")
		params.Set("date", period.Start)
		params.Set("promotionType", "NATIONAL")
		params.Set("category", category)
		path := "/mobile-services/bonuspage/v2/section?" + params.Encode()
		var response section
		if err := c.DoRequest(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, fmt.Errorf("get bonus section %q: %w", category, err)
		}
		for _, item := range response.Items {
			if item.Product != nil {
				key := fmt.Sprintf("product:%d", item.Product.ID)
				if !seen[key] {
					seen[key] = true
					offers = append(offers, makeBonusOffer(item.Product.ID, "", item.Product.Title, item.Product.PriceBefore, item.Product.CurrentPrice, item.Product.BonusMechanism, period))
				}
			}
			if item.Group != nil {
				key := "group:" + item.Group.ID
				if !seen[key] {
					seen[key] = true
					offers = append(offers, makeBonusOffer(0, item.Group.ID, item.Group.Title, item.Group.ExampleFromPrice, item.Group.ExampleForPrice, item.Group.DiscountDescription, period))
				}
			}
		}
	}
	return offers, nil
}

func makeBonusOffer(id int, segmentID, title string, was, now float64, mechanism string, period bonusPeriod) bonusOffer {
	offer := bonusOffer{
		ID:             id,
		BonusSegmentID: segmentID,
		Title:          title,
		OriginalPrice:  was,
		BonusPrice:     now,
		BonusMechanism: mechanism,
		ValidFrom:      period.Start,
		ValidUntil:     period.End,
	}
	if was > 0 && now > 0 {
		offer.DiscountPercentage = (1 - now/was) * 100
	}
	return offer
}

const fetchBonusBoxOffersQuery = `query MCPBonusBoxOffers(
  $filterSet: PromotionsFilterSet,
  $periodStart: String,
  $periodEnd: String,
  $orderId: Int,
  $forcePromotionVisibility: Boolean = true,
  $filterUnavailableProducts: Boolean = false,
  $states: [BonusSegmentState!]
) {
  bonusPersonalPromotionBundles(validOn: $periodStart) {
    maximumActivations
    error
  }
  bonusPromotions(
    filterSet: $filterSet
    input: {
      periodStart: $periodStart
      periodEnd: $periodEnd
      orderId: $orderId
      filterUnavailableProducts: $filterUnavailableProducts
      forcePromotionVisibility: $forcePromotionVisibility
      states: $states
    }
  ) {
    id
    hqId
    title
    activationStatus
    category
    promotionType
    segmentType
    periodStart
    periodEnd
    rawPromotionLabels {
      mechanism
      price
      defaultDescription
    }
    price {
      now { amount }
      was { amount }
    }
  }
}`

func fetchBonusBoxOffers(ctx context.Context, c bonusAPIDoer, period bonusPeriod) (bonusBoxResponse, error) {
	var response struct {
		Bundles []struct {
			MaximumActivations int    `json:"maximumActivations"`
			Error              string `json:"error"`
		} `json:"bonusPersonalPromotionBundles"`
		Promotions []struct {
			ID               string `json:"id"`
			HQID             int    `json:"hqId"`
			Title            string `json:"title"`
			ActivationStatus string `json:"activationStatus"`
			PeriodStart      string `json:"periodStart"`
			PeriodEnd        string `json:"periodEnd"`
			Labels           []struct {
				Mechanism          string  `json:"mechanism"`
				Price              float64 `json:"price"`
				DefaultDescription string  `json:"defaultDescription"`
			} `json:"rawPromotionLabels"`
			Price struct {
				Now struct {
					Amount float64 `json:"amount"`
				} `json:"now"`
				Was struct {
					Amount float64 `json:"amount"`
				} `json:"was"`
			} `json:"price"`
		} `json:"bonusPromotions"`
	}
	variables := map[string]any{
		"filterSet":                 "APP_BONUS_BOX",
		"periodStart":               period.Start,
		"periodEnd":                 period.End,
		"orderId":                   nil,
		"forcePromotionVisibility":  true,
		"filterUnavailableProducts": false,
		"states":                    []string{"ACTIVATABLE", "ACTIVATED"},
	}
	if err := c.DoGraphQL(ctx, fetchBonusBoxOffersQuery, variables, &response); err != nil {
		return bonusBoxResponse{}, fmt.Errorf("get bonus box offers: %w", err)
	}

	out := bonusBoxResponse{ValidFrom: period.Start, ValidUntil: period.End, Offers: make([]bonusBoxOffer, 0, len(response.Promotions))}
	if len(response.Bundles) > 0 {
		out.MaximumActivations = response.Bundles[0].MaximumActivations
	}
	for _, p := range response.Promotions {
		offer := bonusBoxOffer{
			ID:               p.ID,
			BonusSegmentID:   p.ID,
			HQID:             p.HQID,
			Title:            p.Title,
			OriginalPrice:    p.Price.Was.Amount,
			BonusPrice:       p.Price.Now.Amount,
			ValidFrom:        p.PeriodStart,
			ValidUntil:       p.PeriodEnd,
			ActivationStatus: p.ActivationStatus,
			Available:        strings.EqualFold(p.ActivationStatus, "ACTIVATABLE"),
			Activated:        strings.EqualFold(p.ActivationStatus, "ACTIVATED"),
		}
		if offer.ValidFrom == "" {
			offer.ValidFrom = period.Start
		}
		if offer.ValidUntil == "" {
			offer.ValidUntil = period.End
		}
		if len(p.Labels) > 0 {
			offer.BonusMechanism = p.Labels[0].DefaultDescription
			if offer.BonusPrice == 0 && p.Labels[0].Price > 0 {
				offer.BonusPrice = p.Labels[0].Price
			}
		}
		if offer.OriginalPrice > 0 && offer.BonusPrice > 0 {
			offer.DiscountPercentage = (1 - offer.BonusPrice/offer.OriginalPrice) * 100
		}
		out.Offers = append(out.Offers, offer)
	}
	return out, nil
}

type bonusGroupProduct struct {
	ID             int     `json:"id"`
	Title          string  `json:"title"`
	Price          float64 `json:"price"`
	BonusPrice     float64 `json:"bonus_price,omitempty"`
	Unit           string  `json:"unit,omitempty"`
	IsBonus        bool    `json:"is_bonus"`
	BonusMechanism string  `json:"bonus_mechanism,omitempty"`
	ImageURL       string  `json:"image_url,omitempty"`
	ValidFrom      string  `json:"valid_from,omitempty"`
	ValidUntil     string  `json:"valid_until,omitempty"`
}

const fetchBonusGroupProductsForPeriodQuery = `query MCPBonusPromotionWithProducts(
  $id: String,
  $periodStart: String,
  $periodEnd: String,
  $filterUnavailableProducts: Boolean,
  $forcePromotionVisibility: Boolean = true,
  $showAllPromotionSegments: Boolean = true
) {
  bonusPromotions(
    input: {
      id: $id
      periodStart: $periodStart
      periodEnd: $periodEnd
      filterUnavailableProducts: $filterUnavailableProducts
      forcePromotionVisibility: $forcePromotionVisibility
      showAllPromotionSegments: $showAllPromotionSegments
    }
  ) {
    id
    products {
      id
      title
      salesUnitSize
      availability { isOrderable }
      priceV2(
        periodStart: $periodStart
        periodEnd: $periodEnd
        filterUnavailableProducts: $filterUnavailableProducts
        forcePromotionVisibility: true
      ) {
        now { amount }
        was { amount }
        promotionLabel {
          tiers { description }
        }
      }
      imagePack {
        large { url }
      }
    }
  }
}`

func fetchBonusGroupProductsForPeriod(ctx context.Context, c bonusAPIDoer, segmentID string, period bonusPeriod) ([]bonusGroupProduct, error) {
	var response struct {
		Promotions []struct {
			ID       string `json:"id"`
			Products []struct {
				ID            int    `json:"id"`
				Title         string `json:"title"`
				SalesUnitSize string `json:"salesUnitSize"`
				PriceV2       struct {
					Now struct {
						Amount float64 `json:"amount"`
					} `json:"now"`
					Was struct {
						Amount float64 `json:"amount"`
					} `json:"was"`
					PromotionLabel *struct {
						Tiers []struct {
							Description string `json:"description"`
						} `json:"tiers"`
					} `json:"promotionLabel"`
				} `json:"priceV2"`
				ImagePack []struct {
					Large *struct {
						URL string `json:"url"`
					} `json:"large"`
				} `json:"imagePack"`
			} `json:"products"`
		} `json:"bonusPromotions"`
	}
	variables := map[string]any{
		"id":                        segmentID,
		"periodStart":               period.Start,
		"periodEnd":                 period.End,
		"filterUnavailableProducts": true,
		"forcePromotionVisibility":  true,
		"showAllPromotionSegments":  true,
	}
	if err := c.DoGraphQL(ctx, fetchBonusGroupProductsForPeriodQuery, variables, &response); err != nil {
		return nil, fmt.Errorf("get bonus group products: %w", err)
	}
	if len(response.Promotions) == 0 {
		return []bonusGroupProduct{}, nil
	}
	products := make([]bonusGroupProduct, 0, len(response.Promotions[0].Products))
	for _, p := range response.Promotions[0].Products {
		item := bonusGroupProduct{
			ID:         p.ID,
			Title:      p.Title,
			Price:      p.PriceV2.Was.Amount,
			BonusPrice: p.PriceV2.Now.Amount,
			Unit:       p.SalesUnitSize,
			IsBonus:    true,
			ValidFrom:  period.Start,
			ValidUntil: period.End,
		}
		if item.Price == 0 {
			item.Price = item.BonusPrice
		}
		if p.PriceV2.PromotionLabel != nil && len(p.PriceV2.PromotionLabel.Tiers) > 0 {
			item.BonusMechanism = p.PriceV2.PromotionLabel.Tiers[0].Description
		}
		if len(p.ImagePack) > 0 && p.ImagePack[0].Large != nil {
			item.ImageURL = p.ImagePack[0].Large.URL
		}
		products = append(products, item)
	}
	return products, nil
}

type nextWeekBonusResponse struct {
	ValidFrom  string       `json:"valid_from,omitempty"`
	ValidUntil string       `json:"valid_until,omitempty"`
	Offers     []bonusOffer `json:"offers"`
}

func buildNextWeekBonusResponse(ctx context.Context, c bonusAPIDoer, now time.Time, query string, limit int) (nextWeekBonusResponse, error) {
	periods, err := fetchBonusPeriods(ctx, c)
	if err != nil {
		return nextWeekBonusResponse{}, err
	}
	period, ok := selectNextBonusPeriod(periods, now)
	if !ok {
		return nextWeekBonusResponse{Offers: []bonusOffer{}}, nil
	}
	offers, err := fetchBonusOffersForPeriod(ctx, c, period)
	if err != nil {
		return nextWeekBonusResponse{}, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]bonusOffer, 0, len(offers))
	for _, offer := range offers {
		if query != "" && !strings.Contains(strings.ToLower(offer.Title), query) {
			continue
		}
		filtered = append(filtered, offer)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return nextWeekBonusResponse{ValidFrom: period.Start, ValidUntil: period.End, Offers: filtered}, nil
}

func buildCurrentBonusBoxResponse(ctx context.Context, c bonusAPIDoer, now time.Time) (bonusBoxResponse, error) {
	periods, err := fetchBonusPeriods(ctx, c)
	if err != nil {
		return bonusBoxResponse{}, err
	}
	period, ok := selectCurrentBonusPeriod(periods, now)
	if !ok {
		return bonusBoxResponse{Offers: []bonusBoxOffer{}}, nil
	}
	return fetchBonusBoxOffers(ctx, c, period)
}
