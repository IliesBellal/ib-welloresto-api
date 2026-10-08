package fiscalarchive

import (
	"strings"

	"welloresto-api/internal/version"
)

// notice est la notice explicative en français jointe à chaque archive
// (BOI-TVA-DECLA-30-10-30 §230).
func notice() string {
	return strings.ReplaceAll(noticeText, "{logiciel}", version.Product+" "+version.Version)
}

const noticeText = `ARCHIVE FISCALE — NOTICE EXPLICATIVE
Logiciel de caisse : {logiciel}

Cette archive contient, ligne par ligne, les données d'encaissement d'un
établissement sur une période close (article 286, I, 3° bis du CGI ;
BOI-TVA-DECLA-30-10-30, §50 et §220 à §250). Elle est produite par le
logiciel, figée et scellée : sa génération est inscrite dans une chaîne
d'empreintes signée tenue par le logiciel.

1. FORMAT
- Fichiers CSV en UTF-8 (avec marque d'ordre d'octets), séparateur « ; »,
  fins de ligne CRLF, première ligne = noms des colonnes.
- Montants : en centimes (colonnes « _centimes », entiers) et, pour les
  principaux, en euros avec virgule décimale (colonnes « _euros »).
- Dates : heure locale de l'établissement (colonnes « _locale », AAAA-MM-JJ
  HH:MM:SS) et heure universelle (colonnes « _utc », ISO 8601).
- Taux de TVA en pourcentage (ex. « 5,5 », « 10 », « 20 »).
- Les colonnes « _json » contiennent un objet ou une liste au format JSON.
- Textes libres (libellés, commentaires) : quand ils commencent par =, +, -,
  @, une tabulation ou un retour chariot, une apostrophe est ajoutée devant
  pour qu'un tableur ne les interprète pas comme une formule. La valeur
  enregistrée est le texte qui suit cette apostrophe.

2. CONTENU
- tickets.csv : un justificatif par ligne (ticket de vente ou avoir) émis
  dans la période. « numero » est le numéro du justificatif, continu par
  établissement. « canal » est le canal d'encaissement (caisse, borne,
  commande en ligne, plateforme), qui tient lieu de numéro de caisse ;
  « registre_commande » est le registre de caisse de la commande quand elle
  en a un. Un avoir porte un montant négatif.
  « format_lignes » : « complet » — les lignes du ticket comprennent articles,
  options payantes, suppléments, frais de livraison et remises, et leur somme
  vaut la TVA ventilée du ticket ; « historique » — tickets antérieurs, dont
  les lignes ne portent que le prix de chaque article (voir commandes_lignes.csv
  pour le détail).
- tickets_lignes.csv : détail des articles de chaque justificatif (libellé,
  quantité, prix unitaire TTC, taux de TVA, et pour le format complet total
  TTC, HT et TVA de la ligne). « rang_article » relie une option ou un
  supplément à son article.
- tickets_tva.csv : ventilation de chaque justificatif par taux de TVA (TTC,
  HT, TVA), nette des remises de caisse.
- commandes.csv : commandes closes dans la période (date de clôture), avec
  leur statut (vente, annulation, refus...), leurs montants et leur canal.
- commandes_lignes.csv : lignes de ces commandes (articles, options,
  suppléments) telles qu'enregistrées, avec le taux de TVA figé à la vente.
- paiements.csv : règlements enregistrés dans la période (moyen, montant, type
  vente ou remboursement, registre). Un paiement annulé reste présent
  (« actif » = non) : son annulation est tracée dans journal.csv.
- journal.csv : traces de modification et de correction des commandes et des
  paiements (création, mise à jour, clôture, réouverture, annulation,
  remboursement, annulation de paiement), avec l'état avant et après.
- clotures.csv : clôtures journalières de la période, clôture mensuelle et,
  le cas échéant, annuelle : totaux des ventes et des avoirs, TVA par taux,
  règlements, canaux, grand total de la période (depuis le 1er janvier) et
  total perpétuel (depuis l'origine, jamais remis à zéro). La clôture
  journalière contient l'empreinte de chaque commande close ce jour-là.
- registres.csv : registres de caisse fermés dans la période (Z) : ouverture,
  fermeture, fonds de caisse et lignes du Z par moyen de paiement.
- MANIFEST.json : établissement, période, version du logiciel, date de
  génération, et empreinte SHA-256, nombre de lignes et taille de chaque
  fichier.

3. INTÉGRITÉ
- Chaque fichier de l'archive a son empreinte SHA-256 dans MANIFEST.json :
  toute modification d'un fichier se voit en recalculant son empreinte
  (commande « sha256sum » sous Linux et macOS, « certutil -hashfile <fichier>
  SHA256 » sous Windows).
- L'empreinte SHA-256 du fichier ZIP et celle de MANIFEST.json sont inscrites
  par le logiciel dans sa chaîne des archives, au moment de la génération.
- Les colonnes « empreinte », « empreinte_precedente » et « signature »
  reproduisent le chaînage de chaque enregistrement fiscal : chaque
  enregistrement porte l'empreinte du précédent ; l'empreinte est le SHA-256
  d'une sérialisation canonique de ses données, et la signature une
  signature HMAC-SHA256 de l'éditeur. Une rupture de chaînage ou une donnée
  modifiée se détecte en recalculant les empreintes. L'éditeur fournit, à la
  demande de l'administration, l'outil de contrôle des signatures.
- « version_empreinte » : 2 pour les enregistrements écrits avec la méthode
  actuelle (données complètes, signées) ; 1 pour les enregistrements plus
  anciens, dont l'empreinte suit l'ancienne méthode.
`
