CREATE TABLE outbound_messages (
  id varchar(64) NOT NULL,
  channel varchar(16) NOT NULL,
  provider varchar(32) NOT NULL,
  provider_message_id varchar(255) NOT NULL,
  domain varchar(64) NOT NULL,
  domain_ref_id varchar(64) NOT NULL,
  recipient varchar(255) NOT NULL,
  status varchar(32) NOT NULL,
  sent_at datetime NOT NULL DEFAULT UTC_TIMESTAMP(),
  updated_at datetime NOT NULL DEFAULT UTC_TIMESTAMP() ON UPDATE UTC_TIMESTAMP(),
  PRIMARY KEY (id),
  INDEX idx_outbound_messages_provider_message_id (provider_message_id),
  INDEX idx_outbound_messages_domain_ref (domain, domain_ref_id)
);
