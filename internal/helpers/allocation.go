package helpers

import "sort"

// AllocateLargestRemainder répartit total entre des parts proportionnelles aux
// poids (méthode du plus grand reste) : la somme des parts vaut exactement
// total. Les poids négatifs comptent pour 0. Renvoie nil si la somme des poids
// est nulle (rien sur quoi répartir). total peut être négatif (remboursement
// supérieur aux paiements) : la répartition se fait sur sa valeur absolue.
//
// Sert à répartir une remise ou un paiement entre les taux de TVA d'une
// commande (export comptable, registre de caisse) au centime près.
func AllocateLargestRemainder(total int64, weights []int64) []int64 {
	var sum int64
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if sum == 0 {
		return nil
	}
	sign := int64(1)
	if total < 0 {
		sign, total = -1, -total
	}

	shares := make([]int64, len(weights))
	remainders := make([]int64, len(weights))
	var allocated int64
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		shares[i] = total * w / sum
		remainders[i] = total * w % sum
		allocated += shares[i]
	}

	order := make([]int, 0, len(weights))
	for i, w := range weights {
		if w > 0 {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return remainders[order[a]] > remainders[order[b]] })
	for k := 0; allocated < total; k++ {
		shares[order[k%len(order)]]++
		allocated++
	}

	for i := range shares {
		shares[i] *= sign
	}
	return shares
}
