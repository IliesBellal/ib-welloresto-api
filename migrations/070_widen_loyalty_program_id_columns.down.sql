ALTER TABLE customer_loyalty_progress
  MODIFY loyalty_program_id varchar(30) NOT NULL;

ALTER TABLE customer_loyalty_progress_order
  MODIFY loyalty_program_id varchar(30) NOT NULL;

ALTER TABLE customer_rewards
  MODIFY loyalty_program_id varchar(30) NOT NULL;
