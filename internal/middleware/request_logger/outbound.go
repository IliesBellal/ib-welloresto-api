package requestlogger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"welloresto-api/internal/models"
)

// Journal des appels API sortants (Uber Eats, Deliveroo, Brevo, Stripe...) dans
// api_request_logs, au même titre que les appels entrants : docs/OUTBOUND_API_LOGS.md.
//
// Aucune colonne ni migration : une ligne sortante se reconnaît à son URL
// absolue (https://api.brevo.com/...), les lignes entrantes portent un chemin
// relatif (/orders/...). Les en-têtes ne sont jamais stockés (la table n'a pas de
// colonne pour eux), donc jamais d'Authorization ni de clé d'API.
//
// Principe de sûreté : ce code ne doit JAMAIS changer le résultat d'un appel.
// Il ne bloque pas (Logger.Log est non bloquant), ne retourne jamais d'erreur
// de son cru, absorbe ses propres panics, et ne produit que du JSON valide (un
// seul corps invalide ferait échouer tout le lot d'insertion, entrants compris).

const (
	// outboundBodyCap borne le corps stocké (requête et réponse). Au-delà, le
	// début est conservé et marqué tronqué ; l'appelant, lui, reçoit tout.
	outboundBodyCap = 32 * 1024

	truncatedSuffix = "…[truncated]"
)

var defaultLogger atomic.Pointer[Logger]

// SetDefault désigne le Logger qui reçoit les appels sortants. Sans appel
// (tests, outils cmd/), Transport est un simple passe-plat.
func SetDefault(l *Logger) { defaultLogger.Store(l) }

// Transport enveloppe base (http.DefaultTransport si nil) pour journaliser
// chaque appel. Usage : &http.Client{Transport: requestlogger.Transport(nil)}.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &outboundTransport{base: base}
}

type outboundTransport struct {
	base http.RoundTripper
}

func (t *outboundTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	l := defaultLogger.Load()
	if l == nil {
		return t.base.RoundTrip(req)
	}

	startedAt := time.Now()
	skipBodies := isAuthEndpoint(req.URL)

	var reqRaw []byte
	var reqTruncated bool
	if !skipBodies {
		reqRaw, reqTruncated = peekRequestBody(req)
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		record(l, req, reqRaw, reqTruncated, skipBodies, 0, errorPayload(req, err), startedAt)
		return resp, err
	}
	if resp == nil {
		return resp, err
	}

	var respPayload []byte
	switch {
	case skipBodies:
		respPayload = redactedMarker()
	case !isTextual(resp.Header.Get("Content-Type"), nil):
		respPayload = sizeMarker(resp.ContentLength)
	default:
		// Lecture anticipée du début de la réponse, puis restitution intégrale à
		// l'appelant (début relu + suite du flux). Journaliser ici, et non à la
		// fermeture du corps, garantit une ligne même quand l'appelant jette la
		// réponse sans la lire ni la fermer.
		head, truncated, restored := peekResponseBody(resp.Body)
		resp.Body = restored
		respPayload = bodyToJSON(head, truncated, resp.Header.Get("Content-Type"))
	}

	record(l, req, reqRaw, reqTruncated, skipBodies, resp.StatusCode, respPayload, startedAt)
	return resp, nil
}

// record construit et pousse l'entrée. Tout panic est absorbé : le journal ne
// doit jamais faire échouer l'appel métier.
func record(l *Logger, req *http.Request, reqRaw []byte, reqTruncated, skipBodies bool, status int, respPayload []byte, startedAt time.Time) {
	defer func() { _ = recover() }()

	var reqPayload []byte
	if skipBodies {
		reqPayload = redactedMarker()
	} else {
		reqPayload = bodyToJSON(reqRaw, reqTruncated, req.Header.Get("Content-Type"))
	}

	entry := LogEntry{
		Method:          req.Method,
		URL:             sanitizedURL(req.URL),
		Payload:         ensureJSON(reqPayload),
		ResponsePayload: ensureJSON(respPayload),
		StatusCode:      status,
		DurationMs:      time.Since(startedAt).Milliseconds(),
	}

	// Identité de la requête entrante qui a déclenché cet appel, quand le contexte
	// la porte (les appels asynchrones en context.Background() n'en ont pas).
	if id, ok := req.Context().Value(models.ContextRequestIdentity).(*models.RequestIdentity); ok && id != nil {
		if uid, err := strconv.ParseInt(id.UserID, 10, 64); err == nil {
			entry.UserID = &uid
		}
		if id.MerchantID != "" {
			m := id.MerchantID
			entry.MerchantID = &m
		}
	}

	l.Log(entry)
}

// peekRequestBody lit le début du corps via GetBody (copie indépendante : le
// corps réellement envoyé n'est pas touché). Sans GetBody (flux), rien n'est lu.
func peekRequestBody(req *http.Request) (raw []byte, truncated bool) {
	defer func() {
		if recover() != nil {
			raw, truncated = nil, false
		}
	}()
	if req.Body == nil || req.Body == http.NoBody || req.GetBody == nil {
		return nil, false
	}
	rc, err := req.GetBody()
	if err != nil || rc == nil {
		return nil, false
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, outboundBodyCap+1))
	if len(b) > outboundBodyCap {
		return b[:outboundBodyCap], true
	}
	return b, false
}

// peekResponseBody lit jusqu'à outboundBodyCap+1 octets et rend un corps
// équivalent à l'original : le début déjà lu, suivi de la suite du flux, ou de
// l'erreur de lecture si elle est survenue pendant la lecture anticipée.
func peekResponseBody(body io.ReadCloser) (head []byte, truncated bool, restored io.ReadCloser) {
	if body == nil || body == http.NoBody {
		return nil, false, body
	}
	b, err := io.ReadAll(io.LimitReader(body, outboundBodyCap+1))

	var rest io.Reader = body
	if err != nil {
		rest = errReader{err}
	}
	restored = &splicedBody{Reader: io.MultiReader(bytes.NewReader(b), rest), closer: body}

	if len(b) > outboundBodyCap {
		return b[:outboundBodyCap], true, restored
	}
	return b, false, restored
}

type splicedBody struct {
	io.Reader
	closer io.Closer
}

func (s *splicedBody) Close() error { return s.closer.Close() }

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// ── Conversion en JSON valide ────────────────────────────────────────────────

// bodyToJSON rend toujours un document JSON valide : le JSON complet tel quel,
// sinon le texte (tronqué ou non) sous forme de chaîne JSON, sinon un marqueur
// de taille pour le binaire.
func bodyToJSON(raw []byte, truncated bool, contentType string) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	if !isTextual(contentType, raw) {
		return sizeMarker(int64(len(raw)))
	}
	s := redactSecrets(strings.ToValidUTF8(string(raw), ""))
	if !truncated && json.Valid([]byte(s)) {
		return []byte(s)
	}
	if truncated {
		s += truncatedSuffix
	}
	b, err := json.Marshal(s)
	if err != nil {
		return sizeMarker(int64(len(raw)))
	}
	return b
}

// ensureJSON est le dernier filet : jsonb refuse le JSON invalide et la séquence
// \u0000 ; l'un comme l'autre ferait échouer le lot entier d'insertion.
func ensureJSON(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	if !json.Valid(b) || bytes.Contains(b, []byte(`\u0000`)) {
		return sizeMarker(int64(len(b)))
	}
	return b
}

func sizeMarker(n int64) []byte {
	return []byte(fmt.Sprintf(`{"non_json_body_bytes":%d}`, n))
}

func redactedMarker() []byte { return []byte(`{"redacted":"auth endpoint"}`) }

// errorPayload décrit un échec de transport (timeout, DNS, connexion refusée),
// statut 0. L'erreur est débarrassée de la query string pour ne pas y laisser de clé.
func errorPayload(req *http.Request, err error) []byte {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	msg := err.Error()
	if q := req.URL.RawQuery; q != "" {
		msg = strings.ReplaceAll(msg, q, "[REDACTED]")
	}
	b, mErr := json.Marshal(map[string]string{"transport_error": msg})
	if mErr != nil {
		return []byte("{}")
	}
	return b
}

func isTextual(contentType string, raw []byte) bool {
	ct := strings.ToLower(contentType)
	switch {
	case ct == "":
		return raw == nil || (utf8.Valid(raw) && !bytes.Contains(raw, []byte{0}))
	case strings.Contains(ct, "json"), strings.Contains(ct, "text"), strings.Contains(ct, "xml"),
		strings.Contains(ct, "x-www-form-urlencoded"), strings.Contains(ct, "javascript"):
		return !bytes.Contains(raw, []byte{0})
	}
	return false
}

// ── Données sensibles ────────────────────────────────────────────────────────

var (
	secretJSONRe = regexp.MustCompile(`(?i)("(?:client_secret|access_token|refresh_token|id_token|api[_-]?key|password|secret|authorization|assertion)"\s*:\s*)"(?:[^"\\]|\\.)*"`)
	secretFormRe = regexp.MustCompile(`(?i)\b(client_secret|access_token|refresh_token|password|api_key|secret)=[^&\s]*`)
)

func redactSecrets(s string) string {
	s = secretJSONRe.ReplaceAllString(s, `$1"[REDACTED]"`)
	return secretFormRe.ReplaceAllString(s, `$1=[REDACTED]`)
}

// isAuthEndpoint repère les échanges de jetons OAuth (Uber, Deliveroo, Google
// FCM, Pennylane...) : leurs corps portent client_secret et access_token, on n'en
// garde que le statut et la durée.
func isAuthEndpoint(u *url.URL) bool {
	if u == nil {
		return false
	}
	p := strings.ToLower(u.Path)
	return strings.Contains(p, "oauth") || strings.Contains(p, "/token")
}

var sensitiveQueryParts = []string{"token", "key", "secret", "signature", "sig", "password", "code", "auth"}

// sanitizedURL masque les paramètres sensibles de la query (ex. key= de Google
// Maps) et les identifiants éventuels de l'URL.
func sanitizedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.User = nil
	c.Fragment = ""
	if c.RawQuery != "" {
		q, err := url.ParseQuery(c.RawQuery)
		if err != nil {
			c.RawQuery = "[REDACTED]"
		} else {
			changed := false
			for k := range q {
				lk := strings.ToLower(k)
				for _, part := range sensitiveQueryParts {
					if strings.Contains(lk, part) {
						q.Set(k, "[REDACTED]")
						changed = true
						break
					}
				}
			}
			if changed {
				c.RawQuery = q.Encode()
			}
		}
	}
	return c.String()
}
