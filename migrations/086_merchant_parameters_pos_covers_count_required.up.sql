-- Obligation de saisir le nombre de couverts a la creation d'une commande.
--
-- Ne porte que sur les commandes sur place : le nombre de couverts n'a pas de
-- sens pour l'emporte ni la livraison, et la valeur est deja persistee par
-- orders.places_settings (cf. orders.Repository, agregat covers_count du
-- resume de caisse).
--
-- Consomme par le POS : quand le drapeau est actif, la chaine
-- OrderTypeDialog -> CustomerTableLocationDialog enchaine automatiquement sur
-- CustomerCountNumberDialog, pre-rempli avec la somme des places des tables
-- retenues. Le popup reste annulable et la commande reste creable sans
-- couverts : l'obligation porte sur la demande, pas sur la saisie.
--
-- Defaut false pour ne rien changer aux etablissements existants, qui
-- decouvriraient sinon une etape supplementaire sans l'avoir demandee.

ALTER TABLE merchant_parameters
    ADD COLUMN pos_covers_count_required boolean NOT NULL DEFAULT false;
