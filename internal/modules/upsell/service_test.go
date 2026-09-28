package upsell

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu"

	"go.uber.org/zap"
)

func cartOf(ids ...string) []models.ProductEntry {
	cart := make([]models.ProductEntry, 0, len(ids))
	for _, id := range ids {
		cart = append(cart, models.ProductEntry{ProductID: id})
	}
	return cart
}

func TestBuildCartView_ResolvesVariantsToTheirGroup(t *testing.T) {
	// 11 and 12 are two variants of group 1 (e.g. Coca 33cl / Coca 1.25L).
	view := buildCartView(cartOf("11", "12", "5"), map[string]string{"11": "1", "12": "1"})

	wantExcluded := map[string]struct{}{"11": {}, "12": {}, "1": {}, "5": {}}
	if !reflect.DeepEqual(view.excluded, wantExcluded) {
		t.Fatalf("excluded = %v, want %v", view.excluded, wantExcluded)
	}
	if want := []string{"1", "5"}; !reflect.DeepEqual(view.patternKeys, want) {
		t.Fatalf("patternKeys = %v, want %v (one key per group, no duplicate)", view.patternKeys, want)
	}
}

func TestBuildCartView_WithoutGroupsUsesRawIDs(t *testing.T) {
	view := buildCartView(cartOf("5", "7", "5"), nil)

	if want := []string{"5", "7"}; !reflect.DeepEqual(view.patternKeys, want) {
		t.Fatalf("patternKeys = %v, want %v", view.patternKeys, want)
	}
	if len(view.excluded) != 2 {
		t.Fatalf("excluded = %v, want the two raw ids", view.excluded)
	}
}

func TestRankPatternSuggestions_KeepsFewerThanMaxItems(t *testing.T) {
	candidates := map[string]menu.AvailableProduct{
		"a": {ProductID: "a", Name: "Tiramisu", Price: 450},
		"b": {ProductID: "b", Name: "Orangina", Price: 190},
		"c": {ProductID: "c", Name: "Eau", Price: 140},
	}
	// "gone" is no longer a candidate (out of stock, or in the cart).
	aggregated := map[string]float64{"a": 2.0, "b": 3.0, "c": 2.0, "gone": 9.0}

	got := rankPatternSuggestions(aggregated, candidates, 4)

	ids := make([]string, 0, len(got))
	for _, sg := range got {
		ids = append(ids, sg.ProductID)
		if sg.Origin != OriginPattern {
			t.Errorf("%s: origin = %q, want %q", sg.ProductID, sg.Origin, OriginPattern)
		}
	}
	// Best score first, ties broken by product id.
	if want := []string{"b", "a", "c"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if got[0].Name != "Orangina" || got[0].Price != 190 {
		t.Fatalf("first suggestion not enriched from candidate: %+v", got[0])
	}
}

func TestRankPatternSuggestions_CapsAtMaxItems(t *testing.T) {
	candidates := map[string]menu.AvailableProduct{"a": {ProductID: "a"}, "b": {ProductID: "b"}, "c": {ProductID: "c"}}
	got := rankPatternSuggestions(map[string]float64{"a": 1, "b": 2, "c": 3}, candidates, 2)
	if len(got) != 2 || got[0].ProductID != "c" || got[1].ProductID != "b" {
		t.Fatalf("got %+v, want [c b]", got)
	}
}

func TestAppendUnique_SkipsDuplicatesAndStopsAtMax(t *testing.T) {
	list := []SuggestedItem{{ProductID: "a", Origin: OriginPattern}}
	extra := []SuggestedItem{
		{ProductID: "a", Origin: OriginFeatured},
		{ProductID: "b", Origin: OriginFeatured},
		{ProductID: "c", Origin: OriginFeatured},
		{ProductID: "d", Origin: OriginFeatured},
	}

	got := appendUnique(list, extra, 3)

	if len(got) != 3 || got[0].ProductID != "a" || got[1].ProductID != "b" || got[2].ProductID != "c" {
		t.Fatalf("got %+v, want [a b c]", got)
	}
	if got[0].Origin != OriginPattern {
		t.Fatalf("existing item was replaced by its duplicate: %+v", got[0])
	}
}

func TestMergeSetsAndWithoutSuggested_DoNotMutateInputs(t *testing.T) {
	excluded := map[string]struct{}{"cart": {}}
	candidates := map[string]menu.AvailableProduct{"a": {ProductID: "a"}, "b": {ProductID: "b"}}
	chosen := []SuggestedItem{{ProductID: "a"}}

	ex := mergeSets(excluded, map[string]struct{}{"closed": {}})
	cand := withoutSuggested(candidates, chosen)

	if _, ok := ex["closed"]; !ok || len(ex) != 2 {
		t.Fatalf("mergeSets = %v, want cart + closed", ex)
	}
	if _, ok := cand["a"]; ok || len(cand) != 1 {
		t.Fatalf("withoutSuggested = %v, want only b", cand)
	}
	if len(excluded) != 1 || len(candidates) != 2 {
		t.Fatal("inputs were mutated")
	}
}

func TestSourceOf(t *testing.T) {
	cases := []struct {
		name  string
		items []SuggestedItem
		want  string
	}{
		{"empty", nil, SourceNone},
		{"pattern completed", []SuggestedItem{{Origin: OriginPattern}, {Origin: OriginLowPrice}}, SourcePattern},
		{"low price completed", []SuggestedItem{{Origin: OriginLowPrice}, {Origin: OriginLLM}}, SourceLowPrice},
		{"llm only", []SuggestedItem{{Origin: OriginLLM}}, SourceLLM},
		{"featured only", []SuggestedItem{{Origin: OriginFeatured}}, SourceFeaturedFallback},
	}
	for _, c := range cases {
		if got := sourceOf(c.items); got != c.want {
			t.Errorf("%s: sourceOf = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBuildUserPrompt_VariantTakesItsGroupCategory(t *testing.T) {
	available := []menu.AvailableProduct{{ProductID: "1", Name: "Coca Cola", CategoryName: "Boissons"}}
	cart := []models.ProductEntry{{ProductID: "11", Name: "Coca Cola (33cl)"}}

	raw, err := buildUserPrompt(cart, map[string]string{"11": "1"}, available, nil, nil, "")
	if err != nil {
		t.Fatalf("buildUserPrompt: %v", err)
	}

	var payload struct {
		Cart []map[string]string `json:"cart"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal prompt: %v", err)
	}
	if len(payload.Cart) != 1 || payload.Cart[0]["category"] != "Boissons" {
		t.Fatalf("cart = %v, want the variant with its group category", payload.Cart)
	}
}

// fakeProvider records the last request and answers with a fixed content.
type fakeProvider struct {
	content string
	lastReq ai.CompletionRequest
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Complete(_ context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	f.lastReq = req
	return &ai.CompletionResponse{Content: f.content, Model: "fake-model", InputTokens: 10, OutputTokens: 5}, nil
}

func newTestService(t *testing.T, provider ai.LLMProvider, enabled bool) *Service {
	t.Helper()
	reg, err := ai.NewRegistry(ai.AIConfig{
		Tasks: map[string]ai.TaskConfig{upsellTask: {Provider: provider.Name(), Enabled: enabled}},
	}, map[string]ai.LLMProvider{provider.Name(): provider})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return &Service{aiRegistry: reg, logger: zap.NewNop()}
}

func TestLLMSuggestions_FillsOnlyTheRemainingSlots(t *testing.T) {
	provider := &fakeProvider{content: `{"suggestions":[
		{"product_id":"a","title":"Déjà suggéré","score":0.9},
		{"product_id":"b","title":"Un dessert ?","score":0.8},
		{"product_id":"c","title":"Une boisson ?","score":0.7},
		{"product_id":"d","title":"Encore ?","score":0.6}]}`}
	svc := newTestService(t, provider, true)

	// "a" was already chosen by the patterns, so it is not a candidate any more.
	candidates := map[string]menu.AvailableProduct{
		"b": {ProductID: "b", Name: "Tiramisu", Price: 450},
		"c": {ProductID: "c", Name: "Orangina", Price: 190},
		"d": {ProductID: "d", Name: "Eau", Price: 140},
	}

	items, resp, providerName := svc.llmSuggestions(context.Background(), "m1", cartOf("x"), nil, nil, candidates, nil, "", 2)

	if len(items) != 2 || items[0].ProductID != "b" || items[1].ProductID != "c" {
		t.Fatalf("items = %+v, want [b c]", items)
	}
	for _, it := range items {
		if it.Origin != OriginLLM || it.Name == "" {
			t.Fatalf("item not tagged/enriched: %+v", it)
		}
	}
	if resp == nil || resp.Model != "fake-model" || providerName == nil || *providerName != "fake" {
		t.Fatalf("LLM metadata missing: resp=%+v provider=%v", resp, providerName)
	}
	if strings.Contains(provider.lastReq.SystemPrompt, "{MAX_ITEMS}") || !strings.Contains(provider.lastReq.SystemPrompt, "entre 1 et 2 produits") {
		t.Fatalf("system prompt does not carry the remaining slots:\n%s", provider.lastReq.SystemPrompt)
	}
}

func TestLLMSuggestions_DisabledYieldsNothing(t *testing.T) {
	provider := &fakeProvider{content: `{"suggestions":[{"product_id":"b","title":"x","score":1}]}`}
	svc := newTestService(t, provider, false)

	items, resp, _ := svc.llmSuggestions(context.Background(), "m1", cartOf("x"), nil, nil,
		map[string]menu.AvailableProduct{"b": {ProductID: "b"}}, nil, "", 2)

	if items != nil || resp != nil {
		t.Fatalf("disabled LLM returned items=%+v resp=%+v", items, resp)
	}
	if provider.lastReq.Task != "" {
		t.Fatal("provider was called although the task is disabled")
	}
}

func TestLowPriceFromEntries_KeepsOrderAndOnlyCandidates(t *testing.T) {
	entries := []LowPriceEntry{
		{ProductID: "orangina", Price: 190, Orders: 335},
		{ProductID: "in-cart", Price: 140, Orders: 177},
		{ProductID: "magnum", Price: 290, Orders: 53},
		{ProductID: "tiramisu", Price: 300, Orders: 51},
	}
	// "in-cart" is not a candidate any more (in the cart, closed, or not on
	// this channel).
	candidates := map[string]menu.AvailableProduct{
		"orangina": {ProductID: "orangina", Name: "Orangina", Price: 190},
		"magnum":   {ProductID: "magnum", Name: "Magnum", Price: 290},
		"tiramisu": {ProductID: "tiramisu", Name: "Tiramisu", Price: 300},
	}

	got := lowPriceFromEntries(entries, candidates, 2)

	if len(got) != 2 || got[0].ProductID != "orangina" || got[1].ProductID != "magnum" {
		t.Fatalf("got %+v, want [orangina magnum]", got)
	}
	for _, sg := range got {
		if sg.Origin != OriginLowPrice || sg.Name == "" || sg.Title == "" {
			t.Fatalf("item not tagged/enriched: %+v", sg)
		}
	}
}

func TestAvailableOnChannel(t *testing.T) {
	snoOnly := menu.AvailableProduct{IsAvailableOnSNO: true}
	cases := []struct {
		channel string
		want    bool
	}{
		{ChannelPOS, true},
		{ChannelSNO, true},
		{ChannelKiosk, false},
	}
	for _, c := range cases {
		if got := availableOnChannel(snoOnly, c.channel); got != c.want {
			t.Errorf("%s: availableOnChannel = %v, want %v", c.channel, got, c.want)
		}
	}
}

type fakeSchedules struct {
	unavailable map[string]string
	err         error
	calls       int
}

func (f *fakeSchedules) GetUnavailableProductsAt(context.Context, string, time.Time) (map[string]string, error) {
	f.calls++
	return f.unavailable, f.err
}

func TestUnavailableNow_OnlySNOAndKiosk(t *testing.T) {
	schedules := &fakeSchedules{unavailable: map[string]string{"brunch": "Brunch"}}
	svc := &Service{schedules: schedules, logger: zap.NewNop()}

	if got := svc.unavailableNow(context.Background(), "m1", ChannelPOS); got != nil {
		t.Fatalf("POS: got %v, want no schedule filter", got)
	}
	if schedules.calls != 0 {
		t.Fatal("POS must not even look schedules up")
	}
	for _, channel := range []string{ChannelSNO, ChannelKiosk} {
		got := svc.unavailableNow(context.Background(), "m1", channel)
		if _, ok := got["brunch"]; !ok || len(got) != 1 {
			t.Fatalf("%s: got %v, want {brunch}", channel, got)
		}
	}
}

func TestUnavailableNow_LookupFailureFiltersNothing(t *testing.T) {
	svc := &Service{schedules: &fakeSchedules{err: errors.New("db down")}, logger: zap.NewNop()}
	if got := svc.unavailableNow(context.Background(), "m1", ChannelSNO); got != nil {
		t.Fatalf("got %v, want nil on lookup failure", got)
	}
	noSchedules := &Service{logger: zap.NewNop()}
	if got := noSchedules.unavailableNow(context.Background(), "m1", ChannelKiosk); got != nil {
		t.Fatalf("got %v, want nil without a schedule service", got)
	}
}

func TestCartCategories_VariantTakesItsGroupCategory(t *testing.T) {
	available := []menu.AvailableProduct{
		{ProductID: "pizza", CategoryID: "pizzas"},
		{ProductID: "coca", CategoryID: "boissons"},
		{ProductID: "sans-categorie", CategoryID: ""},
	}
	// "coca-33" is a variant of "coca"; "sans-categorie" has no category.
	cart := cartOf("pizza", "coca-33", "sans-categorie")

	got := cartCategories(cart, map[string]string{"coca-33": "coca"}, available)

	want := map[string]struct{}{"pizzas": {}, "boissons": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cartCategories = %v, want %v", got, want)
	}
}

func TestAggregatePatterns_BestConfidenceAndOtherCategoriesOnly(t *testing.T) {
	candidates := map[string]menu.AvailableProduct{
		"orangina": {ProductID: "orangina", CategoryID: "boissons"},
		"tiramisu": {ProductID: "tiramisu", CategoryID: "desserts"},
		"reine":    {ProductID: "reine", CategoryID: "pizzas"},
		"sauce":    {ProductID: "sauce", CategoryID: ""},
	}
	// Two cart products point to their suggestions; "gone" is no longer a
	// candidate.
	lists := [][]PatternEntry{
		{{ProductID: "orangina", Confidence: 0.20}, {ProductID: "reine", Confidence: 0.30}, {ProductID: "gone", Confidence: 0.9}},
		{{ProductID: "orangina", Confidence: 0.12}, {ProductID: "tiramisu", Confidence: 0.15}, {ProductID: "sauce", Confidence: 0.11}},
	}

	got := aggregatePatterns(lists, candidates, nil, map[string]struct{}{"pizzas": {}})

	want := map[string]float64{"orangina": 0.20, "tiramisu": 0.15, "sauce": 0.11}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregatePatterns = %v, want %v (max not sum, no second pizza)", got, want)
	}
}

func TestCheapestVariants(t *testing.T) {
	// Ids as in merchant 212: compared as text, "2340" (Zero) would come
	// before "556" (classic).
	candidates := map[string]menu.AvailableProduct{
		"556":       {ProductID: "556", Name: "Coca Cola (33cl)", GroupID: "coca", Price: 190},
		"2340":      {ProductID: "2340", Name: "Coca Cola Zero (33cl)", GroupID: "coca", Price: 190},
		"554":       {ProductID: "554", Name: "Coca Cola Chery (33cl)", GroupID: "coca", Price: 190},
		"557":       {ProductID: "557", Name: "Coca Cola (1.25L)", GroupID: "coca", Price: 400},
		"fromage-c": {ProductID: "fromage-c", Name: "Pizza Fromage Crème", GroupID: "fromage", Price: 870},
		"fromage-t": {ProductID: "fromage-t", Name: "Pizza Fromage Tomate", GroupID: "fromage", Price: 870},
		"orangina":  {ProductID: "orangina", Name: "Orangina", Price: 190},
	}

	// Best seller among the cheapest variants.
	got := cheapestVariants(candidates, map[string]int{"556": 120, "554": 30, "2340": 45})
	want := map[string]string{"coca": "556", "fromage": "fromage-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cheapestVariants = %v, want %v (cheapest, then best seller, then name)", got, want)
	}

	// Without sales figures: by name, the classic comes first.
	got = cheapestVariants(candidates, nil)
	if got["coca"] != "556" {
		t.Fatalf("cheapestVariants without sales = %v, want coca → 556 (by name)", got)
	}
}

func TestAggregatePatterns_GroupTargetBecomesItsVariant(t *testing.T) {
	// The pattern points to the group "coca" (patterns are computed per
	// group), which is never a candidate itself.
	candidates := map[string]menu.AvailableProduct{
		"coca-33": {ProductID: "coca-33", GroupID: "coca", CategoryID: "boissons"},
	}
	lists := [][]PatternEntry{{{ProductID: "coca", Confidence: 0.25}}}

	got := aggregatePatterns(lists, candidates, map[string]string{"coca": "coca-33"}, nil)
	if want := map[string]float64{"coca-33": 0.25}; !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregatePatterns = %v, want %v", got, want)
	}

	// Drinks already in the cart: the variant is left out like any drink.
	got = aggregatePatterns(lists, candidates, map[string]string{"coca": "coca-33"}, map[string]struct{}{"boissons": {}})
	if len(got) != 0 {
		t.Fatalf("aggregatePatterns = %v, want nothing (category already in the cart)", got)
	}
}
