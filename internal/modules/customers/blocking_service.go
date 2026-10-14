package customers

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// BlockCustomer bloque un client de l'établissement de l'utilisateur pour la
// commande en ligne. Le client est désigné par sa fiche (customer_id) ou par un
// téléphone / email ; sans fiche correspondante, elle est créée pour porter le
// blocage. Toutes les fiches qui partagent le téléphone ou l'email de la cible
// sont bloquées ensemble (doublons compris), sinon un doublon non bloqué
// laisserait passer la commande.
func (s *CustomersService) BlockCustomer(ctx context.Context, req BlockCustomerRequest) (*BlockCustomerResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	comment := strings.TrimSpace(req.Comment)
	if utf8.RuneCountInString(comment) > maxBlockCommentLen {
		comment = truncateRunes(comment, maxBlockCommentLen)
	}
	reasonID := strings.TrimSpace(req.ReasonID)
	if err := s.validateBlockReason(ctx, reasonID, comment); err != nil {
		return nil, err
	}

	identity, err := s.resolveBlockIdentity(ctx, user.MerchantID, req)
	if err != nil {
		return nil, err
	}

	existing, err := s.customerRepo.findCustomerIDsByIdentity(ctx, user.MerchantID, identity)
	if err != nil {
		return nil, err
	}

	created := false
	if len(existing) == 0 {
		// Client inconnu : on crée la fiche qui portera le blocage.
		brand := models.BrandWelloResto
		consent := false
		name := blockedCustomerName(req.FirstName, req.LastName, identity.Tel, identity.Email)
		customer := models.Customer{
			MerchantID:         user.MerchantID,
			CustomerBrand:      &brand,
			AdvertisingConsent: &consent,
			CustomerName:       &name,
		}
		if identity.Tel != "" {
			customer.CustomerTel = &identity.Tel
		}
		if identity.Email != "" {
			customer.CustomerEmail = &identity.Email
		}
		if v := truncateRunes(strings.TrimSpace(req.FirstName), 50); v != "" {
			customer.CustomerFirstName = &v
		}
		if v := truncateRunes(strings.TrimSpace(req.LastName), 50); v != "" {
			customer.CustomerLastName = &v
		}
		if _, err := s.customerRepo.UpdateOrCreateCustomer(ctx, &customer); err != nil {
			return nil, err
		}
		created = true
	}

	ids, err := s.customerRepo.setBlocked(ctx, user.MerchantID, identity, &blockInfo{
		By:       user.UserID,
		ReasonID: reasonID,
		Comment:  comment,
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		// Ne devrait pas arriver (fiche trouvée ou créée juste avant) : mieux
		// vaut une erreur qu'un faux "client bloqué".
		return nil, errors.New("customer block: no customer row updated")
	}

	primary := ids[0]
	if req.CustomerID != "" {
		primary = strings.TrimSpace(req.CustomerID)
	}

	if s.auditService != nil {
		_ = s.auditService.LogChange(ctx, user.MerchantID, user.UserID, "customer_blocked", models.ResourceCustomer, primary, nil, map[string]interface{}{
			"customer_ids": ids,
			"created":      created,
			"reason_id":    reasonID,
			"comment":      comment,
		})
	}

	return &BlockCustomerResult{CustomerID: primary, Created: created, BlockedCount: len(ids)}, nil
}

// UnblockCustomer lève le blocage du client : sa fiche et toutes celles qui
// partagent son téléphone ou son email (le blocage porte sur l'identité, lever
// une seule fiche en doublon laisserait le client bloqué).
func (s *CustomersService) UnblockCustomer(ctx context.Context, customerID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	customerID = strings.TrimSpace(customerID)
	if _, err := strconv.ParseInt(customerID, 10, 64); err != nil {
		return ErrCustomerNotFound
	}
	tel, email, err := s.customerRepo.getCustomerIdentity(ctx, user.MerchantID, customerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCustomerNotFound
	}
	if err != nil {
		return err
	}

	ids, err := s.customerRepo.setBlocked(ctx, user.MerchantID,
		blockIdentity{CustomerID: customerID, Tel: tel, Email: email}, nil)
	if err != nil {
		return err
	}

	if s.auditService != nil {
		_ = s.auditService.LogChange(ctx, user.MerchantID, user.UserID, "customer_unblocked", models.ResourceCustomer, customerID, nil, map[string]interface{}{
			"customer_ids": ids,
		})
	}
	return nil
}

// IsPhoneBlocked : voir CustomersRepository.IsPhoneBlocked. Exposé au module
// scannorder, qui refuse la commande d'un client bloqué.
func (s *CustomersService) IsPhoneBlocked(ctx context.Context, merchantID, phone string) (bool, error) {
	return s.customerRepo.IsPhoneBlocked(ctx, merchantID, phone)
}

// validateBlockReason vérifie que le motif est un motif de blocage actif et que
// le commentaire est fourni quand le motif l'exige.
func (s *CustomersService) validateBlockReason(ctx context.Context, reasonID, comment string) error {
	if reasonID == "" {
		return ErrBlockReasonInvalid
	}
	requiresComment, found, err := s.customerRepo.FindBlockReason(ctx, reasonID)
	if err != nil {
		return err
	}
	if !found {
		return ErrBlockReasonInvalid
	}
	if requiresComment && comment == "" {
		return ErrBlockCommentRequired
	}
	return nil
}

// resolveBlockIdentity détermine les fiches visées : la fiche customer_id (dont
// on reprend téléphone et email, introuvable = ErrCustomerNotFound) ou, sans
// customer_id, le téléphone et/ou l'email saisis (normalisés).
func (s *CustomersService) resolveBlockIdentity(ctx context.Context, merchantID string, req BlockCustomerRequest) (blockIdentity, error) {
	customerID := strings.TrimSpace(req.CustomerID)
	if customerID != "" {
		if _, err := strconv.ParseInt(customerID, 10, 64); err != nil {
			return blockIdentity{}, ErrCustomerNotFound
		}
		tel, email, err := s.customerRepo.getCustomerIdentity(ctx, merchantID, customerID)
		if errors.Is(err, sql.ErrNoRows) {
			return blockIdentity{}, ErrCustomerNotFound
		}
		if err != nil {
			return blockIdentity{}, err
		}
		return blockIdentity{CustomerID: customerID, Tel: tel, Email: email}, nil
	}

	phone, err := normalizeBlockPhone(req.Phone)
	if err != nil {
		return blockIdentity{}, err
	}
	email, err := normalizeBlockEmail(req.Email)
	if err != nil {
		return blockIdentity{}, err
	}
	if phone == "" && email == "" {
		return blockIdentity{}, ErrBlockIdentityRequired
	}
	return blockIdentity{Tel: phone, Email: email}, nil
}
