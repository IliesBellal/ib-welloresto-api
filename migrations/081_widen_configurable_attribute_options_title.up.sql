-- PostgreSQL migration : elargit configurable_attribute_options.title de
-- varchar(25) a varchar(80).
--
-- Pourquoi : 25 caracteres est la plus courte des colonnes de libelle du menu
-- (products.name est en varchar(255), configurable_attributes.title en
-- varchar(80)) et ne tient pas les libelles d'options reels d'un catalogue
-- importe - un intitule comme "Supplement fromage de chevre" fait 28
-- caracteres. En MySQL non-strict le depassement etait tronque silencieusement ;
-- en Postgres il leve une erreur dure 22001, ce qui ferait echouer l'import
-- entier sur un simple libelle un peu long.
--
-- 80 aligne cette colonne sur configurable_attributes.title, dont elle est le
-- pendant cote option - meme nature de donnee, meme largeur.
--
-- Elargir un varchar(n) en varchar(m) avec m > n ne reecrit pas la table en
-- Postgres (changement de typmod seul, pas de rewrite) : l'operation est
-- instantanee et ne prend qu'un ACCESS EXCLUSIVE bref, ce qui compte ici vu que
-- le pool applicatif est plafonne a 1 connexion (internal/database/postgres.go).
--
-- Aucune donnee modifiee : les libelles existants (<= 25 caracteres) restent
-- inchanges.

ALTER TABLE configurable_attribute_options
    ALTER COLUMN title TYPE varchar(80);

COMMENT ON COLUMN configurable_attribute_options.title IS 'Libelle de l''option affiche au client. Elargi de 25 a 80 caracteres (migration 081), aligne sur configurable_attributes.title.';
