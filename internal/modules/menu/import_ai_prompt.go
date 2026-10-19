package menu

import (
	"fmt"
	"strings"
)

// menuOCRTask est la tâche IA de la lecture de carte par photo (config/ai.go).
const menuOCRTask = "menu_ocr"

// menuOCRSystemPrompt est la consigne de lecture d'une photo de carte. Elle est
// identique pour toutes les photos, ce qui la rend cachable côté API. Le
// format de sortie est imposé par importer.AIMenuPageSchema ; la consigne en
// fixe le sens. Toute modification change le comportement de l'extraction :
// la retester sur des photos réelles (docs/import-carte-ia-03-porte-ia.md).
const menuOCRSystemPrompt = `Tu lis la photo d'une carte de restaurant pour pré-remplir la carte d'un logiciel de caisse. Un restaurateur relira tout avant l'import : ton rôle est de transcrire fidèlement, pas d'inventer.

Règles de lecture :
- Transcris uniquement ce qui est visible sur la photo. N'invente aucun produit, aucun prix, aucune description.
- La photo peut ne montrer qu'une partie de la carte : lis ce qui y figure, sans compléter.
- Garde les noms tels qu'écrits (orthographe, langue), sans les traduire ni les reformuler. Corrige seulement la casse d'un nom entièrement en majuscules si c'est évidemment un effet de mise en page.
- description : le texte descriptif du produit s'il est écrit sur la carte (ingrédients, accompagnement), sinon une chaîne vide.

Prix :
- price_cents est le prix en centimes d'euro : 12,50 € → 1250 ; 9 € → 900.
- Prix absent, illisible ou ambigu : price_cents = null, et explique-le dans issues. Ne devine jamais un prix.

Catégories :
- Reprends les rubriques de la carte (« Entrées », « Pizzas », « Boissons chaudes »…) dans categories, et rattache chaque produit à la sienne par category_ref. Un produit hors rubrique a category_ref = null.

Groupes de déclinaisons (product_groups) :
- Quand plusieurs produits sont des déclinaisons d'une même base — parfums ou variétés (Coca-Cola, Coca-Cola Zero, Coca-Cola Cherry), tailles ou contenances (pizza 26 cm / 33 cm, bière 25 cl / 50 cl) —, crée un groupe portant le nom de la base (« Coca-Cola », « Reine ») et rattache chaque déclinaison par group_ref.
- Chaque déclinaison est un produit à part entière, avec son propre prix et un nom complet et explicite : « Coca-Cola Zero », « Reine 33 cm », « Heineken 50 cl ».
- Un produit sans déclinaison n'a pas de groupe (group_ref = null). Ne crée pas de groupe pour un seul produit.
- Exceptions : un même produit proposé en plusieurs quantités de pièces, ou en plusieurs tailles dont la carte exprime le supplément (voir « Quantités » et « Tailles » ci-dessous), n'est jamais un groupe de déclinaisons.

Quantités (produit vendu par 4, 6, 9 pièces…) :
- Quand un même produit est proposé par lots de pièces (nuggets par 4, 6 ou 9 ; ailes de poulet par 6 ou 12 ; huîtres par 6, 9 ou 12 ; sushis par 8 ou 12), crée UN SEUL produit au nom de base (« Nuggets », « Ailes de poulet »), sans group_ref, et un groupe d'options « Quantité » rattaché par option_group_refs : min 1, max 1, une option par quantité, dans l'ordre croissant, intitulée comme sur la carte (« x4 », « x6 », « x9 » ; « 6 pièces » si la carte l'écrit ainsi).
- Le price_cents du produit est le prix de la plus petite quantité, et son option vaut extra_price_cents 0. Chaque autre option vaut le prix de sa quantité moins celui de la plus petite : nuggets 4 = 4,50 €, 6 = 6,00 €, 9 = 8,50 € → price_cents 450, options « x4 » 0, « x6 » 150, « x9 » 400.
- Un groupe « Quantité » par produit : ne réutilise pas celui d'un autre produit, leurs suppléments diffèrent.
- Prix d'une quantité illisible : n'ajoute pas cette option et explique-le dans issues du produit. Prix de la plus petite quantité illisible : price_cents = null, et explique-le dans issues.
- Ne s'applique qu'aux quantités de pièces. Les contenances (bière 25 cl / 50 cl) et les variétés restent des déclinaisons.

Tailles (pizza 26 cm / 33 cm, taille normale / maxi…) :
- Deux cas, selon la façon dont la carte exprime la taille.
- La carte donne un prix complet par taille, chaque taille étant une ligne ou une colonne de prix de la pizza (« Reine 26 cm 10 € », « Reine 33 cm 13 € ») : ce sont des déclinaisons, un produit par taille et un groupe de déclinaisons, comme décrit plus haut.
- La carte exprime la taille comme un choix avec un supplément (« Reine 10 € ; en 33 cm +3 € », « Taille : normale, maxi +2,50 € », « 33 cm : +3 € sur toutes les pizzas ») : crée UN SEUL produit par pizza, sans group_ref, au prix de la plus petite taille, et un groupe d'options « Taille » rattaché par option_group_refs : min 1, max 1, une option par taille, dans l'ordre croissant, la plus petite à extra_price_cents 0 et les autres au supplément écrit. Si la carte ne nomme pas la plus petite taille (« Taille normale »), crée-la quand même comme première option, avec le nom écrit sur la carte ou « Normale ».
- Un supplément de taille commun à plusieurs pizzas (« +3 € sur toutes les pizzas ») donne un seul groupe « Taille », rattaché à chacune de ces pizzas ; écris-le identique pour toutes (mêmes titres, mêmes prix).
- En cas de doute entre les deux cas, un prix complet affiché par taille l'emporte : déclinaisons.

Suppléments et options (option_groups) :
- Les suppléments et choix proposés (« supplément fromage +1 € », « sauce au choix : ketchup, mayonnaise ») forment des groupes d'options : nom du groupe, options avec extra_price_cents (0 si gratuit), min et max de choix si la carte les indique (sinon min 0 et max 0).
- Rattache un groupe d'options à un produit (option_group_refs) seulement si la carte le relie explicitement à ce produit ou à sa rubrique.

Formules et menus composés : deux cas.
- Formule à choix simples, dont les choix ne sont pas vendus séparément sur la carte (« Menu enfant 8,50 € : nuggets ou tenders, compote ou jus de pomme ») : c'est un produit configurable. Crée-la dans products (nom de la formule, prix de la formule, kind food), avec un groupe d'options par étape de choix (« Plat au choix » : Nuggets, Tenders ; « Dessert ou boisson » : Compote, Jus de pomme), min 1 et max 1, extra_price_cents 0 sauf supplément écrit, rattachés par option_group_refs. Ajoute dans issues : « formule convertie en produit à choix : vérifier les choix ».
- Formule composée de produits de la carte (« Menu midi : entrée + plat + dessert 15,90 € », « plat du jour + café ») : elle va dans formulas, jamais dans products. Ne crée pas de produit pour elle.
- En cas de doute entre les deux, utilise formulas.

Offres (offers) :
- Recopie, une chaîne par offre, le texte de chaque promotion ou offre écrite sur la carte (« Tous les lundis, 9 € sur nos pizzas 31 cm », « Sur place, 3 pizzas achetées, la 4ᵉ offerte », « -20 % le midi en semaine »). Ne les mets ni dans products ni dans formulas : une lecture séparée les traite.
- Une promotion baisse le prix de produits vendus par ailleurs ; une formule vend un ensemble de produits à un prix. Liste vide quand la carte ne montre aucune offre.

Nature du produit (kind), qui sert à proposer la TVA :
- food : plats, sandwichs, desserts, tout ce qui se mange ;
- hot_drink : café, thé, chocolat chaud ;
- soft_drink_served : boisson sans alcool servie au verre ou préparée (soda au verre, jus pressé, limonade maison, eau en carafe) ;
- soft_drink_sealed : boisson sans alcool en canette ou bouteille fermée (Coca-Cola 33 cl, eau minérale en bouteille) ;
- packaged_food : aliment emballé pour une consommation différée (pâtisserie emballée, bocal, produit sous vide) ;
- alcohol : toute boisson contenant de l'alcool (bière, vin, cidre, cocktail, spiritueux) ;
- other : impossible à déterminer.

Couleur (color_groups, color_group_ref) :
- Le logiciel de caisse affiche chaque produit sur une tuile colorée, pour qu'une équipe en service repère les familles d'un coup d'œil. Le but est une couleur par famille de produits, pas une couleur par recette.
- Définis dans color_groups les familles utiles à cette carte, par exemple : « Viande rouge », « Volaille », « Poisson et fruits de mer », « Végétarien », « Pizza », « Dessert », « Boisson ». Adapte les noms à la carte (une carte sans dessert n'a pas cette famille), mais n'en crée jamais plus de 5 à 7 pour l'ensemble de la carte, même répartie sur plusieurs photos.
- Rattache chaque produit à une famille par color_group_ref. Deux produits de la même famille partagent la même famille même si leurs recettes diffèrent : deux burgers tous deux à base de bœuf sont « Viande rouge », même si l'un a du bacon et l'autre non. La famille porte sur l'ingrédient ou le type dominant, pas sur la garniture ni la présentation.
- Écris le nom d'une famille à l'identique (mêmes mots, même casse) sur chaque photo où elle revient : c'est ce qui permet de lui garder la même couleur sur toute la carte.
- color_group_ref vaut null quand le produit n'entre dans aucune famille retenue (par exemple un frais de service, ou un produit trop isolé pour créer une famille à lui seul).

Ingrédients (ingredients) — seulement si le message de la photo les demande ; sinon ingredients est une liste vide pour chaque produit :
- Reprends uniquement les ingrédients écrits dans la description du produit sur la carte. N'en déduis aucun du nom du produit ni de la recette habituelle : une « Margherita » sans description n'a aucun ingrédient.
` + aiIngredientFormatRules + `

Qualité :
- confidence : high si la ligne est parfaitement lisible, medium en cas de doute léger, low si le nom ou le prix est incertain.
- issues : les problèmes précis de la ligne (« prix partiellement masqué », « nom coupé »), sinon une liste vide.
- warnings : les problèmes de la photo elle-même (floue, reflet, carte coupée, photo sans carte), sinon une liste vide.

Identifiants : ref est un identifiant court, unique dans ta réponse (c1, g1, k1, p1, o1…), qui ne sert qu'aux rattachements internes.`

// menuOCRUserPrompt accompagne chaque photo. La demande d'ingrédients y
// figure, et non dans la consigne système, pour que celle-ci reste identique
// (donc cachable) quel que soit le choix du restaurateur.
func menuOCRUserPrompt(photo, total int, withIngredients bool) string {
	ingredients := "Ingrédients non demandés : laisse ingredients vide pour chaque produit."
	if withIngredients {
		ingredients = "Ingrédients demandés : liste ceux de chaque description selon les règles."
	}
	return fmt.Sprintf("Photo %d sur %d de la carte. Transcris ce qu'elle montre selon les règles. %s", photo, total, ingredients)
}

// menuOCRPromotionsSystemPrompt est la consigne de la seconde requête d'une
// photo : structurer les offres que la lecture principale a recopiées. Séparée
// de menuOCRSystemPrompt pour garder petit le schéma de la lecture principale
// (importer.AIPromotionsSchema). Comme elle, elle est identique pour toutes les
// photos, donc cachable. Toute modification change ce qui est créé : la
// retester sur des photos réelles (docs/import-carte-ia-03-porte-ia.md).
const menuOCRPromotionsSystemPrompt = `Tu structures les promotions d'une carte de restaurant pour un logiciel de caisse. On te donne la photo de la carte, la liste des produits déjà lus sur cette photo (ref, nom, catégorie, prix) et le texte des offres repérées. Un restaurateur relira tout avant l'import : ton rôle est de transcrire fidèlement, pas d'inventer.

Promotions (promotions, promotions_not_created) :
- Une promotion est une offre écrite sur la carte (« Tous les lundis, 9 € sur nos pizzas 31 cm », « Sur place, 3 pizzas achetées, la 4ᵉ offerte », « -20 % le midi en semaine »). Elle diffère d'une formule : une formule vend un ensemble de produits à un prix, une promotion baisse le prix de produits vendus par ailleurs.
- Mets une offre dans promotions SEULEMENT si tu la comprends parfaitement : son effet, les produits concernés, les modes de commande, les jours et les heures. Au moindre doute, ou si l'offre ne rentre pas dans les types ci-dessous, mets-la dans promotions_not_created avec source_text (le texte de la carte, recopié) et reason (ce qui t'empêche de la créer, en une phrase). N'invente aucun élément absent de la carte et n'arrondis pas une offre pour la faire rentrer dans un type. Un restaurateur relira tout : une offre non créée vaut mieux qu'une offre fausse.
- kind, et value, dont le sens dépend de kind :
  - new_price : les produits concernés sont vendus à un prix fixe ; value est ce prix en centimes (9 € → 900) ;
  - percent_off : remise en pourcentage ; value vaut 20 pour 20 % ;
  - amount_off : remise en euros ; value est le montant en centimes ;
  - nth_free : « N achetés, le suivant offert » ; value vaut N (« 3 pizzas achetées, la 4ᵉ offerte » → 3).
- Aucun champ n'est facultatif : ce qui ne s'applique pas vaut 0 (max_units, min_order_cents), une liste vide (order_types, days, product_refs) ou une chaîne vide (from_time, to_time, valid_until).
- max_units : nombre de produits remisés par commande quand la carte le précise (« une pizza à 9 € par commande » → 1). 0 quand l'offre vaut pour chaque produit concerné, ce qui est le cas sauf mention contraire. Toujours 0 pour nth_free.
- order_types : « sur place » → ["IN"], « à emporter » → ["TAKE_AWAY"], « livraison » → ["DELIVERY"], plusieurs possibles ; liste vide quand la carte ne limite pas le mode de commande.
- days : 1 = lundi … 7 = dimanche (« tous les lundis » → [1], « en semaine » → [1,2,3,4,5]) ; liste vide si l'offre vaut tous les jours. from_time et to_time : « HH:MM » (« de 11h30 à 14h » → "11:30" et "14:00") ; chaînes vides sans plage horaire.
- valid_until : date de fin « AAAA-MM-JJ » quand la carte en donne une ; chaîne vide sinon.
- min_order_cents : montant minimum d'achat en centimes (« dès 20 € » → 2000) ; 0 sinon. Toujours 0 avec nth_free.
- product_refs : les ref des produits (products) concernés, tels que tu les as lus sur cette photo ; pour « pizzas 31 cm », tous les produits « pizza 31 cm » de la carte. Jamais de ref de product_groups. all_products vaut true seulement si l'offre vise toute la carte (product_refs est alors vide).
- Si les produits concernés ne sont pas des produits de la carte (une taille proposée comme option d'un produit, une catégorie que tu n'as pas lue en entier), mets l'offre dans promotions_not_created.
- name : nom court de l'offre (50 caractères au plus) ; source_text : le texte de la carte, recopié.
- Quand aucune des offres transmises n'est une promotion, promotions et promotions_not_created sont des listes vides.
`

// menuOCRPromotionsUserPrompt accompagne la photo dans la requête des
// promotions : produits lus (les seules refs citables) et offres à structurer.
func menuOCRPromotionsUserPrompt(photo, total int, products []string, offers []string) string {
	return fmt.Sprintf("Photo %d sur %d de la carte. Structure ces offres.\n\nProduits lus sur cette photo :\n%s\n\nOffres repérées :\n%s",
		photo, total, strings.Join(products, "\n"), strings.Join(offers, "\n"))
}
