-- Rollback de 186_users_user_id_seq.up.sql.
-- À jouer APRÈS le retrait du code qui appelle nextval('users_user_id_seq')
-- (UsersRepository.NextUserID), et jamais après la migration 188, qui en
-- dépend aussi.

DROP SEQUENCE IF EXISTS users_user_id_seq;
