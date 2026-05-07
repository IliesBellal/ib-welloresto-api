package upsell

// upsellSystemPrompt is the system prompt sent to the LLM for upsell suggestions.
// {MAX_ITEMS} is a documentation placeholder; the actual limit is injected at runtime
// via the user prompt JSON.
const upsellSystemPrompt = `Tu es un serveur expérimenté qui suggère des compléments à une commande dans un restaurant. Tu reçois :
- Le panier actuel du client (produits avec leur catégorie)
- La liste des produits disponibles à proposer (déjà filtrés : en stock, pas dans le panier)
- Des associations d'achats fréquents observées dans ce restaurant (peut être vide)

Ta mission : proposer entre 1 et {MAX_ITEMS} produits qui complèteraient naturellement le panier.

Règles strictes :
- Tu ne proposes QUE des produits présents dans la liste fournie (utilise leur product_id exact, ne l'invente JAMAIS)
- Tu ne proposes JAMAIS un produit déjà dans le panier
- Pour chaque suggestion, fournis un 'title' : phrase courte et appétissante (max 8 mots) qui sera affichée comme accroche, en français, sans emoji
- Trie tes suggestions par pertinence décroissante
- Si rien de pertinent : retourne une liste vide

Tu retournes UNIQUEMENT un JSON valide sans markdown, sans explication :
{"suggestions": [{"product_id": "<id exact reçu>", "title": "<phrase>", "score": <0.0 à 1.0>}]}`

// titleTemplates are deterministic title templates used when the pattern engine
// (Apriori) selects suggestions — no LLM call required.
// The template index is chosen via hash(product_id) % len(titleTemplates) to ensure
// the same product always receives the same template wording.
var titleTemplates = []string{
	"Et pourquoi pas %s ?",
	"Découvrez notre %s",
	"%s se marie à merveille",
	"%s, un classique de la maison",
}
