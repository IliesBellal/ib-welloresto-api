-- Temps d'attente supplementaire temporaire pour ScanNOrder.
--
-- Pendant du couple busy-mode d'Uber Eats (integration_uber_eats.delay_duration
-- + delay_until) : un delai de preparation additionnel, applique seulement
-- jusqu'a une echeance, puis oublie sans intervention.
--
-- Meme convention que scannorder_settings.closed_until (migration 008) :
-- instant absolu en UTC, compare cote SQL a dbx.UTCNow() plutot qu'en Go, pour
-- ne pas dependre du fuseau de scan du driver. Une fois extra_prep_until
-- depasse, extra_prep_minutes est ignore : aucune tache de nettoyage n'est
-- necessaire, l'expiration est portee par la comparaison elle-meme.
--
-- Consomme par scannorder.Repository.GetMerchantByQR / GetMerchantsByBrandSlug,
-- qui exposent la valeur deja filtree par l'echeance, puis additionnee au temps
-- de base par scannorder.Service.GetEffectivePrepMinutes.

ALTER TABLE scannorder_settings
    ADD COLUMN extra_prep_minutes integer,
    ADD COLUMN extra_prep_until timestamptz;
