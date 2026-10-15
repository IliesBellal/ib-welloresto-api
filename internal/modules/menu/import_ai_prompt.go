package menu

import "fmt"

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

Nature du produit (kind), qui sert à proposer la TVA :
- food : plats, sandwichs, desserts, tout ce qui se mange ;
- hot_drink : café, thé, chocolat chaud ;
- soft_drink_served : boisson sans alcool servie au verre ou préparée (soda au verre, jus pressé, limonade maison, eau en carafe) ;
- soft_drink_sealed : boisson sans alcool en canette ou bouteille fermée (Coca-Cola 33 cl, eau minérale en bouteille) ;
- packaged_food : aliment emballé pour une consommation différée (pâtisserie emballée, bocal, produit sous vide) ;
- alcohol : toute boisson contenant de l'alcool (bière, vin, cidre, cocktail, spiritueux) ;
- other : impossible à déterminer.

Ingrédients (ingredients) — seulement si le message de la photo les demande ; sinon ingredients est une liste vide pour chaque produit :
- Reprends uniquement les ingrédients écrits dans la description du produit sur la carte. N'en déduis aucun du nom du produit ni de la recette habituelle : une « Margherita » sans description n'a aucun ingrédient.
- Un ingrédient par élément cité, au singulier, première lettre en majuscule, sans quantité, sans mode de préparation ni adjectif de présentation : « tomates fraîches confites » → « Tomate » ; « 2 œufs bio » → « Œuf » ; « crème fraîche » reste « Crème fraîche ». Écris le même ingrédient de la même façon dans tous les produits.
- Ignore le sel, le poivre, l'huile et l'eau de cuisson, sauf s'ils sont un élément distinctif du plat (« huile de truffe »).
- category : meat (viandes, charcuterie), fish (poissons, fruits de mer), dairy (fromages, lait, crème, beurre, œufs), vegetables (légumes, herbes, champignons), fruits, grocery (féculents, pâtes, riz, farine, épices, sucre, chocolat), sauces (sauces, condiments, vinaigrettes), bakery (pains, pâtes à pizza, viennoiseries), other.
- unit : l'unité naturelle de stock de l'ingrédient — G pour ce qui se pèse (fromage, viande, légumes, farine), CL ou ML pour un liquide (crème, sauce liquide, lait), PCE pour ce qui se compte (œuf, pain burger, tranche, galette).

Qualité :
- confidence : high si la ligne est parfaitement lisible, medium en cas de doute léger, low si le nom ou le prix est incertain.
- issues : les problèmes précis de la ligne (« prix partiellement masqué », « nom coupé »), sinon une liste vide.
- warnings : les problèmes de la photo elle-même (floue, reflet, carte coupée, photo sans carte), sinon une liste vide.

Identifiants : ref est un identifiant court, unique dans ta réponse (c1, g1, p1, o1…), qui ne sert qu'aux rattachements internes.`

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
