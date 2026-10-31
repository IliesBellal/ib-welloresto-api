// Package marketplacepromo répartit sur les lignes d'une commande de plateforme
// (Uber Eats, Deliveroo) la remise que la plateforme a appliquée, pour que la
// somme des lignes vaille ce que le client a réellement payé.
//
// Les plateformes envoient les prix de catalogue ligne par ligne et la remise à
// part. Le ticket, les clôtures fiscales et l'écriture comptable se reconstruisent
// à partir des lignes : sans cette répartition, une promotion « 1 acheté = 1
// offert » laisse des lignes plus chères que le total encaissé. Le résultat se
// pose dans OrderProductPayload.DiscountedPrice (orderitems.price) en gardant le
// prix de catalogue dans Price (orderitems.base_price).
package marketplacepromo

import (
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// Line est une ligne de commande telle que la plateforme l'envoie.
type Line struct {
	Quantity int
	// UnitPrice : prix unitaire en centimes, options payantes comprises.
	UnitPrice int
	// Cap : part du prix unitaire qu'une promotion sur l'article peut retirer
	// (prix hors options payantes). 0 = tout le prix unitaire.
	Cap int
}

// Target désigne des unités que la plateforme dit avoir remisées.
type Target struct {
	Line  int // indice dans les lignes
	Units int
}

// Part est une partie d'une ligne : une ligne dont les unités ne portent pas la
// même remise est scindée en plusieurs parties.
type Part struct {
	Line      int // indice de la ligne d'origine
	Quantity  int
	UnitPrice int // prix de catalogue
	Price     int // prix unitaire après remise
}

// Discounted indique si la partie est remisée.
func (p Part) Discounted() bool { return p.Price != p.UnitPrice }

// Method décrit comment la remise a été répartie (pour les journaux).
type Method string

const (
	MethodNone            Method = "none"             // pas de remise
	MethodTargeted        Method = "targeted"         // unités désignées, montant exact
	MethodTargetedProrata Method = "targeted_prorata" // unités désignées, montant au prorata
	MethodSpread          Method = "spread"           // sur toutes les lignes, au prorata
	MethodRejected        Method = "rejected"         // remise supérieure aux lignes : rien n'est changé
)

// Result est la répartition d'une remise.
type Result struct {
	Parts  []Part
	Method Method
}

// Allocate répartit discount (centimes) en retirant du prix des unités que la
// plateforme désigne (targets) : une unité remisée perd au plus Cap. Si les
// unités désignées ne suffisent pas, ou si la plateforme n'en désigne aucune,
// la remise est répartie sur toutes les lignes au prorata de leur montant.
func Allocate(lines []Line, targets []Target, discount int) Result {
	if discount <= 0 {
		return identity(lines, MethodNone)
	}
	if discount > total(lines) {
		return identity(lines, MethodRejected)
	}

	if units, caps := targeted(lines, targets); units != nil {
		weights := make([]int64, len(lines))
		var sum int64
		for i := range lines {
			weights[i] = int64(units[i]) * int64(caps[i])
			sum += weights[i]
		}
		if sum >= int64(discount) {
			method := MethodTargetedProrata
			amounts := make([]int, len(lines))
			if sum == int64(discount) {
				method = MethodTargeted
				for i := range lines {
					amounts[i] = int(weights[i])
				}
			} else {
				for i, a := range helpers.AllocateLargestRemainder(int64(discount), weights) {
					amounts[i] = int(a)
				}
			}
			return Result{Parts: build(lines, amounts, units), Method: method}
		}
	}

	return Prorata(lines, nil, discount)
}

// Prorata répartit discount sur toutes les lignes au prorata de weights (un
// poids par ligne ; nil ou inutilisable : le montant de chaque ligne).
func Prorata(lines []Line, weights []int64, discount int) Result {
	if discount <= 0 {
		return identity(lines, MethodNone)
	}
	if discount > total(lines) {
		return identity(lines, MethodRejected)
	}

	amounts := prorata(lines, weights, discount)
	if amounts == nil {
		amounts = prorata(lines, nil, discount)
	}
	if amounts == nil {
		return identity(lines, MethodRejected)
	}
	units := make([]int, len(lines))
	for i, l := range lines {
		units[i] = l.Quantity
	}
	return Result{Parts: build(lines, amounts, units), Method: MethodSpread}
}

// prorata renvoie le montant retiré à chaque ligne, ou nil si les poids ne
// permettent pas de répartir sans dépasser le montant d'une ligne.
func prorata(lines []Line, weights []int64, discount int) []int {
	w := weights
	if len(w) != len(lines) {
		w = make([]int64, len(lines))
		for i, l := range lines {
			w[i] = int64(l.Quantity) * int64(l.UnitPrice)
		}
	}
	shares := helpers.AllocateLargestRemainder(int64(discount), w)
	if shares == nil {
		return nil
	}
	amounts := make([]int, len(lines))
	for i, s := range shares {
		if s > int64(lines[i].Quantity)*int64(lines[i].UnitPrice) {
			return nil
		}
		amounts[i] = int(s)
	}
	return amounts
}

// targeted cumule, par ligne, les unités désignées (au plus la quantité de la
// ligne) et le plafond de remise par unité. Renvoie nil si rien n'est désigné.
func targeted(lines []Line, targets []Target) (units, caps []int) {
	units = make([]int, len(lines))
	caps = make([]int, len(lines))
	found := false
	for _, t := range targets {
		if t.Line < 0 || t.Line >= len(lines) || t.Units <= 0 {
			continue
		}
		room := lines[t.Line].Quantity - units[t.Line]
		if room <= 0 {
			continue
		}
		if t.Units < room {
			room = t.Units
		}
		units[t.Line] += room
		found = true
	}
	if !found {
		return nil, nil
	}
	for i, l := range lines {
		caps[i] = l.Cap
		if caps[i] <= 0 || caps[i] > l.UnitPrice {
			caps[i] = l.UnitPrice
		}
	}
	return units, caps
}

// build écrit les parties : amounts[i] est retiré aux units[i] premières unités
// remisables de la ligne i, le plus également possible (au centime).
func build(lines []Line, amounts, units []int) []Part {
	var parts []Part
	add := func(line, qty, unit, price int) {
		if qty <= 0 {
			return
		}
		if n := len(parts); n > 0 && parts[n-1].Line == line && parts[n-1].Price == price {
			parts[n-1].Quantity += qty
			return
		}
		parts = append(parts, Part{Line: line, Quantity: qty, UnitPrice: unit, Price: price})
	}
	for i, l := range lines {
		if amounts[i] <= 0 || units[i] <= 0 {
			add(i, l.Quantity, l.UnitPrice, l.UnitPrice)
			continue
		}
		n := units[i]
		q, r := amounts[i]/n, amounts[i]%n
		add(i, l.Quantity-n, l.UnitPrice, l.UnitPrice)
		add(i, n-r, l.UnitPrice, l.UnitPrice-q)
		add(i, r, l.UnitPrice, l.UnitPrice-q-1)
	}
	return parts
}

func identity(lines []Line, m Method) Result {
	parts := make([]Part, 0, len(lines))
	for i, l := range lines {
		if l.Quantity > 0 {
			parts = append(parts, Part{Line: i, Quantity: l.Quantity, UnitPrice: l.UnitPrice, Price: l.UnitPrice})
		}
	}
	return Result{Parts: parts, Method: m}
}

func total(lines []Line) int {
	t := 0
	for _, l := range lines {
		t += l.Quantity * l.UnitPrice
	}
	return t
}

// Apply écrit la répartition sur les lignes de commande : products est aligné
// sur les lignes passées à Allocate ou Prorata. Le prix de catalogue reste dans
// Price (orderitems.base_price), le prix payé va dans DiscountedPrice
// (orderitems.price) ; une ligne scindée est dupliquée avec sa quantité. Les
// lignes non remisées gardent DiscountedPrice vide.
func (r Result) Apply(products []models.OrderProductPayload) []models.OrderProductPayload {
	out := make([]models.OrderProductPayload, 0, len(r.Parts))
	for _, part := range r.Parts {
		p := products[part.Line]
		p.Quantity = part.Quantity
		if part.Discounted() {
			price := part.Price
			p.DiscountedPrice = &price
		}
		out = append(out, p)
	}
	return out
}

// Total est la somme des parties après remise (centimes).
func (r Result) Total() int {
	t := 0
	for _, p := range r.Parts {
		t += p.Quantity * p.Price
	}
	return t
}
