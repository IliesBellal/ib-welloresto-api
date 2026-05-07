package upsell

import (
	"context"
	"errors"
	"runtime/debug"
	"time"

	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

// Tracker orchestrates asynchronous acceptance tracking after order creation.
// It is the only consumer of Repository that runs outside the HTTP request lifecycle.
type Tracker struct {
	repo   *Repository
	logger *zap.Logger
}

// NewTracker creates a Tracker backed by the given repository and logger.
func NewTracker(repo *Repository, logger *zap.Logger) *Tracker {
	return &Tracker{repo: repo, logger: logger}
}

// TrackAsync records which suggested products were ultimately ordered.
// It is a fire-and-forget operation: the caller returns immediately and the
// work runs in a background goroutine with its own isolated context.
//
// Rules:
//   - Returns immediately with no side-effect when suggestionID is empty.
//   - Never panics: recover() is deferred inside the goroutine.
//   - Errors are only logged; they never propagate to the caller.
//   - parentCtx is intentionally NOT forwarded into the goroutine because it
//     is tied to the HTTP request and will be cancelled once the handler returns.
func (t *Tracker) TrackAsync(
	parentCtx context.Context,
	suggestionID string,
	merchantID string,
	orderID string,
	finalProducts []models.ProductEntry,
) {
	if suggestionID == "" {
		return
	}

	// Capture all values needed inside the goroutine before launching it.
	// This avoids data races on variables that may be mutated by the caller
	// after TrackAsync returns.
	capturedSuggestionID := suggestionID
	capturedMerchantID := merchantID
	capturedOrderID := orderID
	capturedProducts := make([]models.ProductEntry, len(finalProducts))
	copy(capturedProducts, finalProducts)
	capturedRepo := t.repo
	capturedLogger := t.logger

	go func() {
		// Isolated context — independent from the HTTP request lifetime.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		defer func() {
			if r := recover(); r != nil {
				capturedLogger.Error("upsell: panic recovered in TrackAsync",
					zap.String("suggestion_id", capturedSuggestionID),
					zap.String("stack", string(debug.Stack())),
				)
			}
		}()

		// 1. Load the suggestion to verify ownership and retrieve the suggested set.
		suggestion, err := capturedRepo.GetSuggestion(ctx, capturedSuggestionID)
		if err != nil {
			if errors.Is(err, ErrSuggestionNotFound) {
				capturedLogger.Warn("upsell: suggestion not found for tracking",
					zap.String("suggestion_id", capturedSuggestionID),
				)
				return
			}
			capturedLogger.Error("upsell: GetSuggestion failed during tracking",
				zap.String("suggestion_id", capturedSuggestionID),
				zap.Error(err),
			)
			return
		}

		// 2. Ownership check.
		if suggestion.MerchantID != capturedMerchantID {
			capturedLogger.Warn("upsell: merchant mismatch in TrackAsync — possible data integrity issue",
				zap.String("suggestion_id", capturedSuggestionID),
				zap.String("expected_merchant", capturedMerchantID),
				zap.String("actual_merchant", suggestion.MerchantID),
			)
			return
		}

		// 3. Build a lookup set of product_ids present in the suggestion.
		suggestedSet := make(map[string]struct{}, len(suggestion.SuggestedItems))
		suggestedPrices := make(map[string]int64, len(suggestion.SuggestedItems))
		for _, item := range suggestion.SuggestedItems {
			suggestedSet[item.ProductID] = struct{}{}
			suggestedPrices[item.ProductID] = item.Price
		}

		// 4. Accumulate accepted items from finalProducts.
		// Strategy:
		//   - If ProductEntry.Quantity is non-nil and > 0, use it directly.
		//   - Otherwise treat each ProductEntry as 1 unit and count occurrences.
		type accumulator struct {
			quantity  int
			unitPrice int64
		}
		acc := make(map[string]*accumulator)

		for _, pe := range capturedProducts {
			if _, suggested := suggestedSet[pe.ProductID]; !suggested {
				continue
			}
			if _, exists := acc[pe.ProductID]; !exists {
				acc[pe.ProductID] = &accumulator{unitPrice: pe.Price}
			}
			if pe.Quantity != nil && *pe.Quantity > 0 {
				acc[pe.ProductID].quantity += *pe.Quantity
			} else {
				acc[pe.ProductID].quantity++
			}
		}

		acceptedItems := make([]AcceptedItem, 0, len(acc))
		for productID, a := range acc {
			acceptedItems = append(acceptedItems, AcceptedItem{
				ProductID: productID,
				Quantity:  a.quantity,
				UnitPrice: a.unitPrice,
			})
		}

		// 5. Compute revenue impact in euros (storage as DECIMAL(10,2)).
		var totalCentimes int64
		for _, item := range acceptedItems {
			totalCentimes += int64(item.Quantity) * item.UnitPrice
		}
		revenueImpact := float64(totalCentimes) / 100.0

		// 6. Persist.
		recordErr := capturedRepo.RecordAcceptance(ctx, capturedSuggestionID, capturedMerchantID, RecordAcceptanceParams{
			OrderID:       capturedOrderID,
			AcceptedItems: acceptedItems,
			RevenueImpact: revenueImpact,
		})
		if recordErr != nil {
			capturedLogger.Error("upsell: RecordAcceptance failed",
				zap.String("suggestion_id", capturedSuggestionID),
				zap.String("order_id", capturedOrderID),
				zap.Error(recordErr),
			)
			return
		}

		capturedLogger.Info("upsell suggestion tracked",
			zap.String("suggestion_id", capturedSuggestionID),
			zap.String("order_id", capturedOrderID),
			zap.String("merchant_id", capturedMerchantID),
			zap.Int("suggested_count", len(suggestion.SuggestedItems)),
			zap.Int("accepted_count", len(acceptedItems)),
			zap.Float64("revenue_impact", revenueImpact),
		)
	}()
}
