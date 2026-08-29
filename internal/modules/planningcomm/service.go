package planningcomm

import (
	"context"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/modules/outbound"

	"go.uber.org/zap"
)

const planningSMSSender = "Wello Resto"

type outboundRecorder interface {
	RecordOutboundMessageWithContext(ctx context.Context, channel, provider, providerMessageID, domain, domainRefID, recipient string) error
}

type asyncMailerWithMessageID interface {
	SendAsyncWithMessageID(fromName, fromEmail, to, subject, templateName string, data interface{}, onSent func(messageID string))
}

type asyncSMSWithMessageID interface {
	SendSMSAsyncWithMessageID(senderID, phoneNumber, message string, onSent func(messageID string))
}

type Service struct {
	mailer   mailer.Service
	sms      sms.Service
	baseURL  string
	outbound outboundRecorder
	log      *zap.Logger
}

type ShiftSummary struct {
	DayLabel      string
	StartTime     string
	EndTime       string
	PositionLabel string
}

type PublishedWeekMessage struct {
	WeekID        string
	MerchantName  string
	EmployeeID    string
	EmployeeName  string
	EmployeeEmail string
	EmployeePhone string
	WeekLabel     string
	PlanningURL   string
	Shifts        []ShiftSummary
	AllowSMS      bool
	SendInlineSMS bool
}

func New(mail mailer.Service, smsSvc sms.Service, baseURL string, outboundSvc outboundRecorder, log *zap.Logger) *Service {
	return &Service{mailer: mail, sms: smsSvc, baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), outbound: outboundSvc, log: log}
}

func (s *Service) SendPublishedWeek(ctx context.Context, msg PublishedWeekMessage) {
	s.sendEmail(ctx, msg)
	// Paid option gate: when disabled, planning publication stays email-only.
	// No fallback SMS is sent to inactive employees in that case.
	if !msg.AllowSMS {
		return
	}
	s.sendSMS(ctx, msg)
}

func (s *Service) sendEmail(ctx context.Context, msg PublishedWeekMessage) {
	if s.mailer == nil || strings.TrimSpace(msg.EmployeeEmail) == "" {
		return
	}
	data := map[string]interface{}{
		"BrandName":    "Wello Resto",
		"BrandLogoURL": mailer.BrandLogoURL,
		"SupportEmail": mailer.SupportEmail,
		"Year":         time.Now().Year(),
		"MerchantName": msg.MerchantName,
		"EmployeeName": msg.EmployeeName,
		"WeekLabel":    msg.WeekLabel,
		"PlanningURL":  s.planningURL(msg),
		"Shifts":       msg.Shifts,
	}
	if trackedMailer, ok := s.mailer.(asyncMailerWithMessageID); ok {
		trackedMailer.SendAsyncWithMessageID(msg.MerchantName, mailer.InvoiceEmail, msg.EmployeeEmail, "Votre planning a été publié", "planning_week_published.html", data, func(messageID string) {
			s.recordOutbound(ctx, outbound.ChannelEmail, msg.WeekID, messageID, msg.EmployeeEmail)
		})
		return
	}
	s.mailer.SendAsync(msg.MerchantName, mailer.InvoiceEmail, msg.EmployeeEmail, "Votre planning a été publié", "planning_week_published.html", data)
}

func (s *Service) sendSMS(ctx context.Context, msg PublishedWeekMessage) {
	if s.sms == nil || strings.TrimSpace(msg.EmployeePhone) == "" {
		return
	}
	normalizedPhone := helpers.NormalizePhoneNumber(msg.EmployeePhone, "FR")
	text := s.linkSMS(msg)
	if msg.SendInlineSMS {
		text = s.inlineSMS(msg)
	}
	if trackedSMS, ok := s.sms.(asyncSMSWithMessageID); ok {
		trackedSMS.SendSMSAsyncWithMessageID(planningSMSSender, normalizedPhone, text, func(messageID string) {
			s.recordOutbound(ctx, outbound.ChannelSMS, msg.WeekID, messageID, normalizedPhone)
		})
		return
	}
	s.sms.SendSMSAsync(planningSMSSender, normalizedPhone, text)
}

func (s *Service) linkSMS(msg PublishedWeekMessage) string {
	url := s.planningURL(msg)
	if url == "" {
		return fmt.Sprintf("Planning publie chez %s pour %s. Consultez vos mails où accédez à votre planning dans l'app Wello Resto.", msg.MerchantName, msg.WeekLabel)
	}
	return fmt.Sprintf("Planning publie chez %s pour %s: %s", msg.MerchantName, msg.WeekLabel, url)
}

func (s *Service) inlineSMS(msg PublishedWeekMessage) string {
	if len(msg.Shifts) == 0 {
		return fmt.Sprintf("Planning publie chez %s pour %s. Aucun shift planifie.", msg.MerchantName, msg.WeekLabel)
	}
	parts := make([]string, 0, len(msg.Shifts))
	for _, shift := range msg.Shifts {
		line := fmt.Sprintf("%s %s-%s", shift.DayLabel, shift.StartTime, shift.EndTime)
		if strings.TrimSpace(shift.PositionLabel) != "" {
			line += " " + shift.PositionLabel
		}
		parts = append(parts, line)
	}
	return fmt.Sprintf("Planning publie chez %s pour %s: %s", msg.MerchantName, msg.WeekLabel, strings.Join(parts, "; "))
}

func (s *Service) recordOutbound(ctx context.Context, channel, weekID, providerMessageID, recipient string) {
	if s.outbound == nil || strings.TrimSpace(weekID) == "" || strings.TrimSpace(providerMessageID) == "" {
		return
	}
	if err := s.outbound.RecordOutboundMessageWithContext(ctx, channel, "brevo", providerMessageID, "planning", weekID, recipient); err != nil && s.log != nil {
		s.log.Warn("planningcomm outbound tracking failed", zap.String("channel", channel), zap.String("week_id", weekID), zap.Error(err))
	}
}

func (s *Service) planningURL(msg PublishedWeekMessage) string {
	if strings.TrimSpace(msg.PlanningURL) != "" {
		return strings.TrimSpace(msg.PlanningURL)
	}
	if strings.TrimSpace(s.baseURL) == "" || strings.TrimSpace(msg.WeekID) == "" {
		return ""
	}
	return fmt.Sprintf("%s/planning/weeks/%s", s.baseURL, msg.WeekID)
}
