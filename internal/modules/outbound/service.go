package outbound

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

type repository interface {
	Insert(ctx context.Context, params CreateParams) error
	FindStatusByProviderMessageID(ctx context.Context, providerMessageID string) (string, bool, error)
	UpdateStatusByProviderMessageID(ctx context.Context, providerMessageID, status string) error
}

type Service struct {
	repo repository
	log  *zap.Logger
}

func NewService(repo repository, log *zap.Logger) *Service {
	return &Service{repo: repo, log: log}
}

func (s *Service) RecordOutboundMessage(channel, provider, providerMessageID, domain, domainRefID, recipient string) error {
	return s.RecordOutboundMessageWithContext(context.Background(), channel, provider, providerMessageID, domain, domainRefID, recipient)
}

func (s *Service) RecordOutboundMessageWithContext(ctx context.Context, channel, provider, providerMessageID, domain, domainRefID, recipient string) error {
	status := StatusSent
	if err := s.repo.Insert(ctx, CreateParams{
		Channel:           channel,
		Provider:          provider,
		ProviderMessageID: providerMessageID,
		Domain:            domain,
		DomainRefID:       domainRefID,
		Recipient:         recipient,
		Status:            status,
	}); err != nil {
		return fmt.Errorf("insert outbound message: %w", err)
	}
	return nil
}

func (s *Service) UpdateStatusByProviderMessageID(ctx context.Context, providerMessageID, nextStatus string) (bool, error) {
	providerMessageID = strings.TrimSpace(providerMessageID)
	nextStatus = NormalizeStatus(nextStatus)

	if providerMessageID == "" {
		return false, nil
	}
	if !IsStatusKnown(nextStatus) {
		if s.log != nil {
			s.log.Warn("outbound message: unknown status", zap.String("status", nextStatus))
		}
		return false, nil
	}

	currentStatus, found, err := s.repo.FindStatusByProviderMessageID(ctx, providerMessageID)
	if err != nil {
		return false, fmt.Errorf("find outbound message by provider_message_id: %w", err)
	}
	if !found {
		return false, nil
	}

	if !ShouldAdvanceStatus(currentStatus, nextStatus) {
		return true, nil
	}

	if err := s.repo.UpdateStatusByProviderMessageID(ctx, providerMessageID, nextStatus); err != nil {
		return true, fmt.Errorf("update outbound message status: %w", err)
	}
	return true, nil
}
