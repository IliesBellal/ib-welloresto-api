package auth

import (
	"sync"
	"time"
)

// failedLoginGuard compte, par adresse IP, les connexions par mot de passe
// échouées sur une fenêtre fixe (failedLoginWindow). Au-delà de
// failedLoginsPerIP, l'adresse reçoit un 429 jusqu'à la fenêtre suivante :
// sans ce seuil, des essais lancés en parallèle échappent au délai de
// waitAfterFailedLogin. Le compte visé n'est jamais bloqué (décision Ilies) :
// la même personne se connecte depuis une autre adresse sans attendre.
//
// Chaque essai réserve sa place avant de vérifier le mot de passe, et la rend
// s'il réussit : une rafale parallèle ne passe donc pas d'un coup sous le
// seuil, et les connexions réussies ne comptent jamais. Compteur en mémoire,
// propre à chaque instance de l'API.
type failedLoginGuard struct {
	mu     sync.Mutex
	window int64
	counts map[string]int
}

func newFailedLoginGuard() *failedLoginGuard {
	return &failedLoginGuard{counts: map[string]int{}}
}

// reserve compte un essai pour ip ; false si l'adresse a atteint le seuil. Un
// guard nil accepte tout.
func (g *failedLoginGuard) reserve(ip string, now time.Time) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.roll(now)
	if g.counts[ip] >= failedLoginsPerIP {
		return false
	}
	g.counts[ip]++
	return true
}

// release rend la place d'un essai qui n'a pas échoué. Sans effet si la
// fenêtre a changé depuis reserve : les compteurs sont déjà repartis de zéro.
func (g *failedLoginGuard) release(ip string, reservedAt, now time.Time) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if failedLoginWindowOf(reservedAt) != failedLoginWindowOf(now) {
		return
	}
	g.roll(now)
	if g.counts[ip] > 1 {
		g.counts[ip]--
	} else {
		delete(g.counts, ip)
	}
}

// roll repart de zéro à chaque nouvelle fenêtre : la mémoire reste bornée aux
// adresses vues pendant la fenêtre en cours.
func (g *failedLoginGuard) roll(now time.Time) {
	if w := failedLoginWindowOf(now); w != g.window {
		g.window = w
		g.counts = map[string]int{}
	}
}

func failedLoginWindowOf(t time.Time) int64 {
	return t.UnixNano() / int64(failedLoginWindow)
}
