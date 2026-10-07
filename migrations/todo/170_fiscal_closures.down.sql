-- Reverts 170_fiscal_closures.up.sql. À n'appliquer qu'après avoir retiré du
-- code déployé la tâche de clôture fiscale : supprimer la table efface les
-- clôtures scellées, que le BOI interdit de purger (§170) — en production,
-- les archiver d'abord.
DROP TABLE IF EXISTS fiscal_closures;
