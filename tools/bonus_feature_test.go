package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

type bonusFeatureFakeAPI struct {
	responses []any
	variables []map[string]any
}

func (f *bonusFeatureFakeAPI) DoRequest(_ context.Context, method, _ string, _ any, result any) error {
	if method != http.MethodGet || len(f.responses) == 0 {
		return nil
	}
	data, _ := json.Marshal(f.responses[0])
	f.responses = f.responses[1:]
	return json.Unmarshal(data, result)
}

func (f *bonusFeatureFakeAPI) DoGraphQL(_ context.Context, _ string, variables map[string]any, result any) error {
	f.variables = append(f.variables, variables)
	if len(f.responses) == 0 {
		return nil
	}
	data, _ := json.Marshal(f.responses[0])
	f.responses = f.responses[1:]
	return json.Unmarshal(data, result)
}

func TestBonusFeatureSelectsPublishedFuturePeriod(t *testing.T) {
	periods := []bonusPeriod{
		{Start: "2026-08-31", End: "2026-09-06"},
		{Start: "2026-09-07", End: "2026-09-13"},
	}
	got, ok := selectNextBonusPeriod(periods, time.Date(2026, 9, 5, 0, 0, 0, 0, ahAmsterdamLocation))
	if !ok || got.Start != "2026-09-07" || got.End != "2026-09-13" {
		t.Fatalf("next period = %#v, %v", got, ok)
	}
}

func TestBonusFeatureReturnsEmptyWhenFuturePeriodIsNotPublished(t *testing.T) {
	periods := []bonusPeriod{{Start: "2026-08-31", End: "2026-09-06"}}
	if _, ok := selectNextBonusPeriod(periods, time.Date(2026, 9, 5, 0, 0, 0, 0, ahAmsterdamLocation)); ok {
		t.Fatal("unexpected future period")
	}
}

func TestBonusFeatureBoxMapsActivationState(t *testing.T) {
	api := &bonusFeatureFakeAPI{responses: []any{map[string]any{
		"bonusPersonalPromotionBundles": []any{map[string]any{"maximumActivations": 10}},
		"bonusPromotions": []any{
			map[string]any{"id": "a", "title": "A", "activationStatus": "ACTIVATABLE"},
			map[string]any{"id": "b", "title": "B", "activationStatus": "ACTIVATED"},
		},
	}}}
	box, err := fetchBonusBoxOffers(context.Background(), api, bonusPeriod{Start: "2026-08-31", End: "2026-09-06"})
	if err != nil {
		t.Fatal(err)
	}
	if box.MaximumActivations != 10 || len(box.Offers) != 2 {
		t.Fatalf("box = %#v", box)
	}
	if !box.Offers[0].Available || box.Offers[0].Activated || box.Offers[1].Available || !box.Offers[1].Activated {
		t.Fatalf("activation states = %#v", box.Offers)
	}
	if len(api.variables) != 1 || api.variables[0]["filterSet"] != "APP_BONUS_BOX" {
		t.Fatalf("variables = %#v", api.variables)
	}
}

func TestBonusFeatureRegistersDistinctTools(t *testing.T) {
	s := server.NewMCPServer("test", "test")
	deps := Deps{IsAuthenticated: func() bool { return false }}
	RegisterProductTools(s, deps)
	RegisterBonusTools(s, deps)
	tools := s.ListTools()

	if tools["ah_get_bonus_offers_next_week"] == nil || tools["ah_get_bonus_box"] == nil || tools["ah_get_bonus_group_products"] == nil {
		t.Fatal("new Bonus tools are not registered")
	}
	if !strings.Contains(strings.ToLower(tools["ah_get_bonus_offers_next_week"].Tool.Description), "next week") {
		t.Fatal("next-week tool description is ambiguous")
	}
	if !strings.Contains(strings.ToLower(tools["ah_get_bonus_box"].Tool.Description), "personal") {
		t.Fatal("Bonus Box description is not personal/account-bound")
	}
	schema, _ := json.Marshal(tools["ah_get_bonus_group_products"].Tool.InputSchema)
	if !strings.Contains(string(schema), "valid_from") || !strings.Contains(string(schema), "valid_until") {
		t.Fatalf("group schema is not period-aware: %s", schema)
	}
}
