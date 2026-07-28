package brevo_events

import (
	"context"
	"strings"
	"welloresto-api/internal/modules/outbound"

	"go.uber.org/zap"
)

type statusUpdater interface {
	UpdateStatusByProviderMessageID(ctx context.Context, providerMessageID, nextStatus string) (bool, error)
}

type Service struct {
	outbound statusUpdater
	log      *zap.Logger
}

func NewService(outboundService statusUpdater, log *zap.Logger) *Service {
	return &Service{outbound: outboundService, log: log}
}

func (s *Service) ProcessEvent(ctx context.Context, payload BrevoEventPayload) error {
	event := strings.ToLower(strings.TrimSpace(payload.Event))
	nextStatus, ok := brevoEventToOutboundStatus(event)
	if !ok {
		if s.log != nil {
			s.log.Info("brevo events webhook: ignored event", zap.String("event", payload.Event))
		}
		return nil
	}

	providerMessageID := payload.ProviderMessageID()
	if strings.TrimSpace(providerMessageID) == "" {
		if s.log != nil {
			s.log.Warn("brevo events webhook: missing provider_message_id", zap.String("event", payload.Event))
		}
		return nil
	}

	found, err := s.outbound.UpdateStatusByProviderMessageID(ctx, providerMessageID, nextStatus)
	if err != nil {
		return err
	}
	if !found && s.log != nil {
		s.log.Warn("brevo events webhook: unknown provider_message_id", zap.String("provider_message_id", providerMessageID), zap.String("event", payload.Event))
	}

	return nil
}

func brevoEventToOutboundStatus(event string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "sent":
		return outbound.StatusSent, true
	case "delivered":
		return outbound.StatusDelivered, true
	case "opened", "uniqueopened":
		return outbound.StatusOpened, true
	case "click":
		return outbound.StatusClicked, true
	case "hardbounce", "softbounce":
		return outbound.StatusBounced, true
	case "blocked":
		return outbound.StatusFailed, true
	case "unsubscribed":
		return outbound.StatusUnsubscribed, true
	default:
		return "", false
	}
}
