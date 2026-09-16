# Contrat Stripe Terminal server-driven (source de vérité API ↔ wello-kiosk)

Toutes les routes : préfixe /kiosk/terminal, middleware KioskAuth.

GET    /readers                 → 200 {"readers": [Reader]}
GET    /reader                  → 200 {"reader": Reader | null}
       null si non appairé OU si le reader n'existe plus chez Stripe (l'appairage est alors effacé).
PUT    /reader {"reader_id"}    → 200 {"reader": Reader}
DELETE /reader                  → 204
POST   /payment {"order_id"}    + header Idempotency-Key (UUID par tap) → 200 PaymentStatus
POST   /payment/cancel {"order_id"}      → 200 PaymentStatus (état RÉEL après tentative)
GET    /payment/status?order_id=         → 200 PaymentStatus

Reader = {"id","label","serial_number","status":"online"|"offline"}

PaymentStatus = {
  "order_id", "payment_intent_id",
  "status": "waiting_for_card"|"processing"|"succeeded"|"failed"|"canceled",
  "failure_code": string|null,
  "failure_message": string|null,   // FR, prêt à afficher, produit par le serveur
  "card_present": {"brand","last4","application_preferred_name",
                   "dedicated_file_name","authorization_code"} | null   // renseigné si succeeded
}

Normalisation (serveur uniquement, le client n'interprète jamais Stripe).
Principe directeur : **dans le doute, "processing", jamais "failed"** — un
faux "failed" invite le client à relancer un paiement peut-être déjà en
cours ; un faux "processing" coûte juste une itération de polling de plus.
Règles évaluées dans cet ordre (la première qui matche gagne) :
1. PI succeeded, ou statut local CAPTURED / TO_REFUND              → succeeded
2. PI processing / requires_capture                                → processing
3. PI canceled                                                     → canceled
4. action reader in_progress sur ce PI                             → waiting_for_card
5. action reader succeeded sur ce PI, mais PI pas encore succeeded
   (fenêtre transitoire entre la fin de l'action et la mise à jour
   du PI/du statut local)                                          → processing
6. action reader failed sur ce PI, ou last_payment_error présent   → failed (retryable ; failure_code = celui de l'action sinon celui de last_payment_error)
7. PI requires_payment_method sans action en cours (aucune action
   n'a jamais été dispatchée sur ce PI — diagnostic net, pas une
   ambiguïté transitoire)                                          → failed, failure_code "reader_action_missing"
8. tout autre état Stripe inattendu (ex. requires_confirmation/
   requires_action sans action en cours)                           → processing, failure_code null

## Dispatch (POST /payment)

Règle de commit : un échec côté reader (offline / busy / toute erreur Stripe
au moment du dispatch) ne remet **jamais** en cause le PaymentIntent déjà
résolu ni son mapping déjà écrit en base (order_id/kiosk_id) — seule une
erreur de résolution du PaymentIntent lui-même (avant tout contact avec le
reader) est traitée comme un échec complet de l'appel.

Avant tout dispatch, kiosk_id est posé sur la ligne du PaymentIntent (et son
statut local remis à un état actif s'il avait été marqué FAILED par une
tentative précédente sur le même PI réutilisé) — ces deux écritures
précèdent l'appel Stripe qui déclenche réellement le reader.

Vérification pré-dispatch (état live du reader) :
- une action est déjà in_progress sur CE PI → aucun redispatch, l'appel est
  un no-op idempotent (retourne l'état courant, équivalent à /payment/status).
- une action est in_progress sur un AUTRE PI (commande abandonnée puis
  relancée) → le serveur annule d'abord cette action ; si l'annulation
  échoue, l'appel est refusé (kiosk_terminal_reader_busy) plutôt que de
  risquer deux actions concurrentes sur le même reader physique.

Tout appel Stripe fait pendant qu'un verrou/une transaction est tenu (la
résolution du PaymentIntent, la vérification pré-dispatch, le dispatch
lui-même) est borné à 10 secondes.

Retry après failed : POST /payment sur le même order_id → même PI réutilisé.

## Push WebSocket

WebSocket /ws-kiosk : {"type":"terminal_payment_update", ...PaymentStatus}
Envoyé UNIQUEMENT à la borne concernée (stripe_payments.kiosk_id), sur :
terminal.reader.action_failed, payment_intent.succeeded, payment_intent.payment_failed, payment_intent.canceled.

**Règle de recalcul** : pour tout event autre que `payment_intent.succeeded`
(action_failed, payment_failed, canceled), le serveur ne pousse jamais le
statut brut tiré de cet event — il **recalcule** l'état via le même chemin
que `GET /payment/status` et pousse ce résultat recalculé. Raison : un event
d'échec peut arriver après qu'un retry sur le même PaymentIntent réutilisé
ait déjà relancé une action reader — la règle 4 de normalisation (action
in_progress → waiting_for_card) doit alors l'emporter sur l'échec, désormais
périmé, que cet event à lui seul semblerait signaler. Si le recalcul échoue,
rien n'est poussé (le client retombe sur son polling). Pour
`payment_intent.succeeded`, le statut local qui vient d'être posé suffit —
pas de recalcul nécessaire.

Règle d'annulation : cancel_action d'abord. Le PI n'est annulé que si cancel_action a réussi
ou s'il n'y avait aucune action — si cancel_action échoue pour toute autre raison, le PI
n'est pas touché. Jamais annulé si le PI est succeeded/processing/requires_capture.
Retour = PaymentStatus réel.

Erreurs (code / HTTP) :
kiosk_terminal_not_configured 424 (existant) · kiosk_terminal_location_not_configured 424 ·
kiosk_terminal_reader_not_found 404 · kiosk_terminal_reader_location_mismatch 400 ·
kiosk_terminal_reader_not_paired 424 · kiosk_terminal_reader_offline 424 ·
kiosk_terminal_reader_busy 409 · kiosk_terminal_reader_already_paired 409 ·
kiosk_terminal_payment_conflict 409 (existant) · kiosk_terminal_payment_not_found 404
(GET /payment/status ou /payment/cancel appelé sans qu'aucun PaymentIntent
n'existe encore pour cette commande — POST /payment doit avoir réussi au
moins une fois avant).

Dev uniquement : POST /test/present-payment-method {"order_id","outcome":"success"|"declined"}
→ test_helpers present_payment_method sur le reader appairé.
Répond 404 si la clé Stripe n'est pas une clé de test (sk_test_ ou rk_test_).