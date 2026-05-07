package translation

import "strings"

// ProtectedGastronomyTerms lists French gastronomic terms that must remain
// unchanged in translated output.
var ProtectedGastronomyTerms = []string{
	"magret",
	"confit",
	"foie gras",
	"tartare",
	"rillettes",
	"andouillette",
	"blanquette",
	"bouillabaisse",
	"ratatouille",
	"cassoulet",
	"pot-au-feu",
	"choucroute",
	"quenelle",
	"gratin dauphinois",
	"crème brûlée",
	"mille-feuille",
	"tarte tatin",
	"financier",
	"madeleine",
	"profiterole",
	"baguette",
	"croissant",
	"pain au chocolat",
	"brioche",
	"kouign-amann",
}

func formatProtectedTerms() string {
	return strings.Join(ProtectedGastronomyTerms, ", ")
}
