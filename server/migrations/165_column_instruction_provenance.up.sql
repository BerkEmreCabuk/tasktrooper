-- catalog_sha tracks whose write agent_column_instructions.instruction last
-- reflects: sha256 hex of the text as last written BY THE CATALOG SYNC, or ''
-- for operator-owned/unknown. Sync compares this against sha256(instruction):
-- equal means the row is still exactly what the sync last put there, so a
-- newer catalog version may overwrite it; unequal means an operator changed
-- it through the HTTP API since, and it must never be touched or deleted
-- again on the operator's behalf. Without this a stale catalog row was
-- indistinguishable from an operator's own edit and was skipped forever.
ALTER TABLE agent_column_instructions
    ADD COLUMN IF NOT EXISTS catalog_sha TEXT NOT NULL DEFAULT '';

-- Backfill: a row is catalog-written if its current text equals any version
-- the catalog ever shipped for a columns/*.md file (every commit in this
-- repo's history, plus the working tree, hashed the same way the parser
-- normalizes a column instruction — front matter stripped, trimmed). A text
-- that never matches a shipped version was typed by an operator and must
-- keep catalog_sha = '' so it is never overwritten or deleted by a sync.
UPDATE agent_column_instructions
SET catalog_sha = encode(sha256(convert_to(instruction, 'UTF8')), 'hex')
WHERE encode(sha256(convert_to(instruction, 'UTF8')), 'hex') IN (
    '059c45ec140138ea7469a8b318a26bc8c978a742af9043fe8d924f706a6a7cf4',
    '0ac599b55732dd049fe0718e8dec7b59450d8e804c04dfcd94d14d8174099d9b',
    '0e790d6aeb5933ffa9cdd68b8217893a468d154494aee9449f4d877531e7b545',
    '19f3d77e4ec510be097bfc73f3bbc439135bde823cda09a3e5eb31e00348b2f5',
    '2640b60e0e0253f097d52113ca9180b98077d32c2e6302affe2c50881598414d',
    '2b452a67f2c8e572972a6245bff450e245740463fcc246799a32bd1db2b2c946',
    '546ca179e136dc5a2dd372dad8a089f5d40c5accdd915c3699b9caea1d544775',
    '555726b5db79d1f23f33db278a40a243071c7b4a9bc00542b3b4dde63f0319f0',
    '61b6b23add7403ca7ae7178a4f35e432aaf458f075216580f8531a91fa129eeb',
    '63b87444cc1c069b14612b6a6c9e6677715fea1f72fcb9a6e680f770be6240c5',
    '77df3b490341ea02873fb9ec507ab82748c67f39db796f55967ef0a0fc43d146',
    'a9868540cb7e3d957ff1d2077445b0c0d6d0cc26434edf7f3befa3264e9f9fe2',
    'bb1122cb306254219cd63629c350d46f3116b01f178b3845c7f4c0a5bdb92001',
    'c71518c6fa8b6dc56907075bcd8991ef69f54239c9474b779bbb139df1a15d12',
    'c7cc009b6628544bfc74d41dbae8ca981110787b8a18f65eace47bdc6c53011e',
    'd259b2faee34ffbf96050c366e251111b683a463d5ec4d13a4fbd38df24fadcc',
    'd774ea1a03c6938e72146b8a600d0e12b12c84345c2e2d4da6edb5ac0d3a8af9',
    'd84e6f7bde0b5c7e893afbec40ba5c84a424a19b7dfa36c4c5a255da0231fd8e',
    'f36e47157f97c22c8651c9d23271b38c0b3c8e1294fba3231b541699eddd1b33',
    'f67e2690303abbb3be72d80604db108db65d8dacfd74b637a2ea7a16e9b80b80'
);
