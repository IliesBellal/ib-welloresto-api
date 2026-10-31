package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"welloresto-api/internal/helpers"
	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

// Longueurs de configurable_attributes.title et de configurable_attribute_options.title.
const (
	attributeTitleMax = 80
	optionTitleMax    = 80
)

// mapModifiers rattache les options choisies d'un article aux groupes et options du
// catalogue, en créant ceux qui n'existent pas encore. Le chemin courant (groupe et
// option déjà mappés) est une simple lecture.
//
// Une option ou un groupe qui ne peut pas être créé ne fait pas échouer la commande :
// il est renvoyé dans degraded, sous forme de texte à mettre dans le commentaire de la
// ligne (le prix des options est déjà dans le prix unitaire d'Uber Eats). Seule une
// erreur de lecture, signe d'une base en difficulté, est renvoyée : la nouvelle
// tentative d'Uber la traitera.
func (s *Service) mapModifiers(ctx context.Context, merchantID, productID string, item ueModels.UberCartItem) (cfg *ordersModels.ProductConfiguration, degraded []string, err error) {

	if len(item.SelectedModifierGroups) == 0 {
		return nil, nil, nil
	}

	var attrs []ordersModels.ConfigurationAttribute

	for _, group := range item.SelectedModifierGroups {

		attrID, err := s.resolveAttribute(ctx, merchantID, productID, group)
		if err != nil {
			return nil, nil, err
		}
		if attrID == "" {
			degraded = append(degraded, optionsLine(group, group.SelectedItems))
			continue
		}

		var options []ordersModels.ConfigurationOption
		var lost []ueModels.UberModifierOption

		for _, opt := range group.SelectedItems {
			optID, err := s.resolveOption(ctx, merchantID, attrID, opt)
			if err != nil {
				return nil, nil, err
			}
			if optID == "" {
				lost = append(lost, opt)
				continue
			}

			options = append(options, ordersModels.ConfigurationOption{
				ID:         optID,
				Quantity:   opt.Quantity,
				ExtraPrice: opt.Price.UnitPrice.Amount,
			})
		}

		if len(lost) > 0 {
			degraded = append(degraded, optionsLine(group, lost))
		}
		if len(options) > 0 || len(group.SelectedItems) == 0 {
			attrs = append(attrs, ordersModels.ConfigurationAttribute{
				ID:      attrID,
				Options: options,
			})
		}
	}

	if len(attrs) == 0 {
		return nil, degraded, nil
	}
	return &ordersModels.ProductConfiguration{Attributes: attrs}, degraded, nil
}

// resolveAttribute renvoie l'attribut du groupe d'options, créé avec son mapping (une
// transaction, un verrou par groupe) s'il n'existe pas. "" : la création a échoué, le
// groupe passe en texte.
func (s *Service) resolveAttribute(ctx context.Context, merchantID, productID string, group ueModels.UberModifierGroup) (string, error) {
	existing, err := s.attributeMappingRepo.GetAttributeIDByModifierGroupID(ctx, merchantID, group.ID)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return *existing, nil
	}

	var attrID string
	createErr := s.inLockedTx(ctx, "uber-group:"+merchantID+":"+group.ID, func(txCtx context.Context) error {
		again, err := s.attributeMappingRepo.GetAttributeIDByModifierGroupID(txCtx, merchantID, group.ID)
		if err != nil {
			return err
		}
		if again != nil {
			attrID = *again
			return nil
		}

		newID, err := s.attributeMappingRepo.CreateAttributeFromUberGroup(txCtx, merchantID, helpers.TruncateRunes(group.Title, attributeTitleMax), productID)
		if err != nil {
			return err
		}
		if err := s.attributeMappingRepo.CreateAttributeMapping(txCtx, merchantID, newID, group.ID); err != nil {
			return err
		}
		attrID = newID
		return nil
	})
	if createErr != nil {
		recordDegraded(ctx, merchantID, "modifier_group", group.ID, group.Title, createErr)
		return "", nil
	}
	return attrID, nil
}

// resolveOption renvoie l'option du catalogue pour un choix Uber Eats, créée avec son
// mapping si besoin. "" : la création a échoué, l'option passe en texte.
func (s *Service) resolveOption(ctx context.Context, merchantID, attrID string, opt ueModels.UberModifierOption) (string, error) {
	existing, err := s.attributeMappingRepo.GetOptionIDByUberItemID(ctx, attrID, opt.ID)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return *existing, nil
	}

	var optID string
	createErr := s.inLockedTx(ctx, "uber-option:"+merchantID+":"+opt.ID, func(txCtx context.Context) error {
		again, err := s.attributeMappingRepo.GetOptionIDByUberItemID(txCtx, attrID, opt.ID)
		if err != nil {
			return err
		}
		if again != nil {
			optID = *again
			return nil
		}

		newID, err := s.attributeMappingRepo.CreateOptionFromUber(txCtx, attrID, helpers.TruncateRunes(opt.Title, optionTitleMax), opt.Price.UnitPrice.Amount)
		if err != nil {
			return err
		}
		if err := s.attributeMappingRepo.CreateOptionMapping(txCtx, merchantID, newID, opt.ID); err != nil {
			return err
		}
		optID = newID
		return nil
	})
	if createErr != nil {
		recordDegraded(ctx, merchantID, "modifier_option", opt.ID, opt.Title, createErr)
		return "", nil
	}
	return optID, nil
}

// optionsAsText met toutes les options choisies d'un article en texte, pour l'article
// de repli qui n'a pas d'options structurées.
func optionsAsText(groups []ueModels.UberModifierGroup) []string {
	var lines []string
	for _, g := range groups {
		if len(g.SelectedItems) > 0 {
			lines = append(lines, optionsLine(g, g.SelectedItems))
		}
	}
	return lines
}

// optionsLine : « Options Base : Tomate, Mozzarella ×2 ».
func optionsLine(group ueModels.UberModifierGroup, opts []ueModels.UberModifierOption) string {
	labels := make([]string, 0, len(opts))
	for _, o := range opts {
		label := strings.TrimSpace(o.Title)
		if o.Quantity > 1 {
			label += " ×" + strconv.Itoa(o.Quantity)
		}
		labels = append(labels, label)
	}
	title := strings.TrimSpace(group.Title)
	if title == "" {
		return fmt.Sprintf("Options : %s", strings.Join(labels, ", "))
	}
	return fmt.Sprintf("Options %s : %s", title, strings.Join(labels, ", "))
}
