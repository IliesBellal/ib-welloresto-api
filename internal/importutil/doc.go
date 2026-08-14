// Package importutil regroupe les briques d'import de fichiers sans
// dépendance métier : lecture de classeurs .xlsx et de fichiers CSV en
// [][]string, normalisation de libellés/en-têtes textuels, et le type
// d'erreur générique qui situe une ligne et une colonne fautives.
//
// Ce paquet ne connaît ni produit, ni client, ni aucune entité Wello : il est
// partagé entre internal/modules/menu/importer et
// internal/modules/customers/importer, qui restent indépendants l'un de
// l'autre et branchent chacun leurs propres règles métier en aval de ces
// helpers.
package importutil
