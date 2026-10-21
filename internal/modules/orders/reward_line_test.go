package orders

import (
	"testing"

	"welloresto-api/internal/models"
)

// Une récompense « produit offert » est portée par reward_id (champ propre).
// Tant que models.RewardLineInDiscountID vaut true, le même identifiant est
// aussi renvoyé dans discount_id pour les POS qui ne connaissent pas reward_id
// (clé de fusion des lignes du panier). Les lignes ne partagent pas de pointeur.
func TestApplyRewards_FreeProductCarriesRewardID(t *testing.T) {
	// applyRewards ignore une récompense dont la valeur est nulle ; elle devient
	// le prix remisé de la ligne.
	price := 1
	rewards := []*models.DBReward{{
		RewardID:    "85",
		RewardType:  "free_product",
		RewardValue: &price,
		ProductIDs:  []string{"7"},
	}}

	req := &models.PricingRequest{Order: &models.OrderRequest{OrderType: "IN"}}
	products := []models.OrderProductPayload{
		{ProductID: "7", Quantity: 1, Price: 300},
		{ProductID: "7", Quantity: 1, Price: 300},
	}

	(&OrdersService{}).applyRewards(req, products, rewards)

	// Un seul exemplaire du produit est offert, l'autre reste payant.
	if products[0].RewardID == nil || *products[0].RewardID != "85" {
		t.Fatalf("reward line must carry reward_id 85, got %v", products[0].RewardID)
	}
	if products[0].DiscountedPrice == nil || *products[0].DiscountedPrice != 1 {
		t.Fatalf("reward line must be discounted, got %v", products[0].DiscountedPrice)
	}
	if products[1].RewardID != nil || products[1].DiscountID != nil || products[1].DiscountedPrice != nil {
		t.Fatalf("the second unit must stay a normal line, got %+v", products[1])
	}
	if models.RewardLineInDiscountID {
		if products[0].DiscountID == nil || *products[0].DiscountID != "85" {
			t.Fatalf("transition: discount_id must repeat the reward id, got %v", products[0].DiscountID)
		}
		if products[0].DiscountID == products[0].RewardID {
			t.Fatal("discount_id and reward_id must not share a pointer")
		}
	} else if products[0].DiscountID != nil {
		t.Fatalf("a reward line must not carry a discount_id, got %q", *products[0].DiscountID)
	}
	if len(req.Order.UsedRewards) != 1 || req.Order.UsedRewards[0].RewardID != "85" {
		t.Fatalf("used_rewards must list the reward, got %+v", req.Order.UsedRewards)
	}
}
