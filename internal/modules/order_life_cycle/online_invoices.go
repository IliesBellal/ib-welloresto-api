package order_life_cycle

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/outbound"

	"go.uber.org/zap"
)

// Envoi automatique de la facture des commandes en ligne (docs/emails-styles.md,
// « Facture automatique ») : une commande Wello Resto payée en ligne par Stripe,
// close, d'au moins 25 € TTC et dont le client a laissé son e-mail reçoit sa
// facture PDF (la même que l'envoi depuis la caisse). La tâche
// RunOnlineOrderInvoices appelle SendOnlineOrderInvoices toutes les 5 minutes.
const (
	// OnlineInvoiceMinTTC (centimes) : seuil de la note obligatoire en
	// restauration (25 € TTC) ; en dessous, le client la demande en caisse.
	OnlineInvoiceMinTTC = 2500
	// OnlineInvoiceDomain marque l'envoi dans outbound_messages : une commande
	// qui y figure n'est jamais refacturée.
	OnlineInvoiceDomain = "order_invoice"
	// onlineInvoiceWindow : seules les commandes closes depuis moins de 2 h
	// sont facturées. Évite d'envoyer l'historique au déploiement et borne les
	// nouvelles tentatives d'une commande en échec.
	onlineInvoiceWindow = 2 * time.Hour
	onlineInvoiceBatch  = 100
)

type onlineInvoiceMailer interface {
	SendOrderInvoice(to string, data mailer.InvoiceEmailData, attachment mailer.Attachment) (string, error)
}

type invoiceOutboundRecorder interface {
	RecordOutboundMessageWithContext(ctx context.Context, channel, provider, providerMessageID, domain, domainRefID, recipient string) error
}

type invoiceBrandResolver interface {
	Resolve(ctx context.Context, merchantID, name string) mailer.MerchantBrand
}

// SetOnlineInvoices branche l'envoi automatique des factures : journal des
// messages envoyés (anti-doublon) et identité des établissements.
func (s *OrdersLifeCycleService) SetOnlineInvoices(outboundRec invoiceOutboundRecorder, brands invoiceBrandResolver) {
	s.invoiceOutbound = outboundRec
	s.invoiceBrands = brands
}

// OnlineInvoiceResult résume un passage de SendOnlineOrderInvoices.
type OnlineInvoiceResult struct {
	Sent   int
	Failed int
}

type onlineInvoiceCandidate struct {
	OrderID      string
	MerchantID   string
	CustomerID   string
	CustomerName string
	Email        string
}

// SendOnlineOrderInvoices envoie la facture des commandes en ligne closes
// depuis moins de 2 h et pas encore facturées. Une commande en échec est
// retentée au passage suivant, tant qu'elle reste dans la fenêtre.
func (s *OrdersLifeCycleService) SendOnlineOrderInvoices(ctx context.Context, now time.Time) (OnlineInvoiceResult, error) {
	var res OnlineInvoiceResult
	sender, ok := s.mailerService.(onlineInvoiceMailer)
	if !ok || s.invoiceOutbound == nil {
		return res, errors.New("online invoices: mailer or outbound journal not configured")
	}
	candidates, err := s.listOnlineOrdersToInvoice(ctx, now.Add(-onlineInvoiceWindow))
	if err != nil {
		return res, err
	}
	for _, c := range candidates {
		if err := s.sendOnlineOrderInvoice(ctx, sender, c); err != nil {
			res.Failed++
			logger.FromContext(ctx).Warn("online invoice not sent",
				zap.String("order_id", c.OrderID), zap.String("merchant_id", c.MerchantID), zap.Error(err))
			continue
		}
		res.Sent++
	}
	return res, nil
}

func (s *OrdersLifeCycleService) sendOnlineOrderInvoice(ctx context.Context, sender onlineInvoiceMailer, c onlineInvoiceCandidate) error {
	orders, err := s.ordersService.ComputeGetOrder(ctx, c.MerchantID, c.OrderID)
	if err != nil {
		return err
	}
	if orders == nil || len(orders.Orders) == 0 {
		return models.ErrNotFound
	}
	order := orders.Orders[0]

	inv, err := s.orderInvoicePDF(ctx, c.MerchantID, c.OrderID, &order, c.CustomerID, c.CustomerName)
	if err != nil {
		return err
	}

	messageID, err := s.sendOrderInvoiceMail(ctx, sender, c.MerchantID, c.Email, c.CustomerName, inv)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	// Mail parti : un échec d'écriture ici ferait renvoyer la facture au
	// passage suivant, d'où l'erreur explicite.
	if err := s.journalOrderInvoice(ctx, c.OrderID, c.Email, messageID); err != nil {
		return fmt.Errorf("invoice sent but not journaled (risk of a second send): %w", err)
	}
	return nil
}

// sendOrderInvoiceMail envoie la facture au nom et aux couleurs de
// l'établissement et renvoie le messageId Brevo. Partagé par l'envoi
// automatique et l'envoi depuis la caisse.
func (s *OrdersLifeCycleService) sendOrderInvoiceMail(ctx context.Context, sender onlineInvoiceMailer, merchantID, to, customerName string, inv *orderInvoice) (string, error) {
	brand := mailer.NewMerchantBrand(inv.MerchantName, "", "", "")
	if s.invoiceBrands != nil {
		brand = s.invoiceBrands.Resolve(ctx, merchantID, inv.MerchantName)
	}
	data := mailer.InvoiceEmailData{
		Brand:         brand,
		MerchantName:  inv.MerchantName,
		CustomerName:  customerName,
		ReceiptNumber: inv.ReceiptNumber,
		OrderNumber:   inv.OrderNum,
		SupportEmail:  mailer.SupportEmail,
	}
	return sender.SendOrderInvoice(to, data, mailer.Attachment{Name: inv.FileName, Content: inv.PDF})
}

// journalOrderInvoice inscrit l'envoi d'une facture dans outbound_messages
// (domaine order_invoice) : la commande n'est plus facturée automatiquement.
func (s *OrdersLifeCycleService) journalOrderInvoice(ctx context.Context, orderID, to, messageID string) error {
	if s.invoiceOutbound == nil {
		return errors.New("outbound journal not configured")
	}
	if strings.TrimSpace(messageID) == "" {
		messageID = "order-invoice-" + orderID + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return s.invoiceOutbound.RecordOutboundMessageWithContext(ctx, outbound.ChannelEmail, "brevo", messageID, OnlineInvoiceDomain, orderID, to)
}

// listOnlineOrdersToInvoice lit les commandes à facturer : Wello Resto (ni Uber
// Eats ni Deliveroo), closes depuis since, payées au moins en partie en ligne
// par Stripe (paiement inscrit par le webhook), ticket de vente d'au moins
// OnlineInvoiceMinTTC, client avec e-mail, pas déjà facturées.
func (s *OrdersLifeCycleService) listOnlineOrdersToInvoice(ctx context.Context, since time.Time) ([]onlineInvoiceCandidate, error) {
	db := dbx.GetDB(ctx, s.db)
	rows, err := db.QueryContext(ctx, `
		SELECT o.order_id::text, o.merchant_id, c.customer_id::text,
		       TRIM(COALESCE(c.customer_first_name, '') || ' ' || COALESCE(c.customer_last_name, '')),
		       TRIM(c.customer_email)
		FROM orders o
		JOIN customer c ON c.customer_id = o.customer_id AND c.merchant_id = o.merchant_id
		WHERE o.state = 'CLOSED'
		  AND o.delivered_on >= ?
		  AND o.brand = ?
		  AND TRIM(COALESCE(c.customer_email, '')) <> ''
		  AND EXISTS (SELECT 1 FROM payments p
		              WHERE p.order_id = o.order_id AND p.enabled = TRUE AND p.user_id = ?)
		  AND EXISTS (SELECT 1 FROM receipts r
		              WHERE r.order_id = o.order_id AND r.total_ttc >= ?)
		  AND NOT EXISTS (SELECT 1 FROM outbound_messages om
		                  WHERE om.domain = ? AND om.domain_ref_id = o.order_id::text)
		ORDER BY o.delivered_on
		LIMIT ?`,
		since, models.BrandWelloResto, models.StripeWebhookUserID, OnlineInvoiceMinTTC, OnlineInvoiceDomain, onlineInvoiceBatch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []onlineInvoiceCandidate
	for rows.Next() {
		var c onlineInvoiceCandidate
		if err := rows.Scan(&c.OrderID, &c.MerchantID, &c.CustomerID, &c.CustomerName, &c.Email); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
