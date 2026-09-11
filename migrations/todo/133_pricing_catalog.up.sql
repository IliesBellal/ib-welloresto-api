-- LOT A Semaine 3, Chantier 11a (docs/decisions.md) : table de référence
-- tarifaire. La grille était définie à trois endroits (site vitrine, modèle
-- économique, table packages) — cette table en devient la seule source ;
-- packages (colonnes stripe_price_id + flags fonctionnels) n'a jamais porté
-- de prix et continue de ne pas en porter.
--
-- kind='plan' : les trois offres (essentiel/pro/complet). package_name
-- pointe vers packages.package_name plutôt qu'un id figé, pour rester
-- robuste à des id auto-incrémentés différents entre environnements — voir
-- pricing.Repository.GetPackageIDByName.
-- kind='module' : les briques à la carte (reservation/haccp/planning/
-- marketplaces/delivery). per_unit_price_cents/per_unit_label ne sont
-- renseignés que pour planning (+250/salarié).
-- kind='addon' : poste supplémentaire et les trois paliers de borne
-- (données de référence uniquement dans ce chantier — voir
-- docs/decisions.md pour ce qui est/n'est pas construit dessus).
CREATE TABLE pricing_catalog (
    id                      text PRIMARY KEY,
    kind                    text NOT NULL,
    code                    text NOT NULL,
    package_name            text,
    label                   text NOT NULL,
    monthly_price_cents     integer NOT NULL,
    annual_price_cents      integer,
    included_modules_count  integer,
    per_unit_price_cents    integer,
    per_unit_label          text,
    created_at              timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, code)
);
COMMENT ON COLUMN pricing_catalog.annual_price_cents IS 'Tarif mensuel équivalent sous engagement annuel (ex. "7900 / 6600 annuel" du §1.3) — pas un forfait annuel unique. NULL pour kind=module/addon : aucun tarif annuel distinct n''a été donné pour ces lignes.';
COMMENT ON COLUMN pricing_catalog.included_modules_count IS 'kind=plan uniquement. 0 = aucun module possible sur ce plan (essentiel) ; NULL = tous les modules inclus sans distinction (complet) ; une valeur positive = nombre de modules choisis inclus gratuitement (pro).';

INSERT INTO pricing_catalog (id, kind, code, package_name, label, monthly_price_cents, annual_price_cents, included_modules_count) VALUES
('price-plan-essentiel', 'plan', 'essentiel', 'Essentiel', 'Essentiel', 7900, 6600, 0),
('price-plan-pro',       'plan', 'pro',       'Pro',       'Pro',       12900, 10800, 2),
('price-plan-complet',   'plan', 'complet',   'Complet',   'Complet',   18900, 15800, NULL);

INSERT INTO pricing_catalog (id, kind, code, label, monthly_price_cents, per_unit_price_cents, per_unit_label) VALUES
('price-module-reservation',   'module', 'reservation',   'Réservation',   5900, NULL, NULL),
('price-module-haccp',         'module', 'haccp',         'HACCP',         3500, NULL, NULL),
('price-module-planning',      'module', 'planning',      'Planning',      2900, 250, 'salarié'),
('price-module-marketplaces',  'module', 'marketplaces',  'Marketplaces',  3900, NULL, NULL),
('price-module-delivery',      'module', 'delivery',      'Delivery',      3900, NULL, NULL);

-- Bornes : confirmé avec l'utilisateur (2026-09-11) — 1ère borne 18900/mois
-- les 24 premiers mois puis 8900 ; chaque borne supplémentaire 13900/mois
-- les 24 premiers mois, puis 8900 également (les deux convergent vers le
-- même tarif après amortissement). Données de référence seulement — voir
-- docs/decisions.md, aucune logique de facturation ne consomme ces lignes
-- dans ce chantier (les bornes ne font pas partie du panier "pack le moins
-- cher", chantier 11b).
INSERT INTO pricing_catalog (id, kind, code, label, monthly_price_cents) VALUES
('price-addon-extra-seat',            'addon', 'extra_seat',            'Poste supplémentaire',                        2500),
('price-addon-kiosk-first-tier1',     'addon', 'kiosk_first_tier1',     'Borne (1ère, mois 1-24)',                      18900),
('price-addon-kiosk-additional-tier1','addon', 'kiosk_additional_tier1','Borne supplémentaire (mois 1-24)',             13900),
('price-addon-kiosk-tier2',           'addon', 'kiosk_tier2',           'Borne, toute borne (après 24 mois)',           8900);
