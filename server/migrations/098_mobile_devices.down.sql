-- Reverses 098 by rebuilding 097's single-row table and carrying the oldest
-- registration back into it.
--
-- Oldest, not "the one that matters": going back to one row means one phone
-- survives, and the first-registered one is the only choice this migration can
-- make without guessing. The rest are lost, which is the honest outcome of
-- reverting to a schema that cannot hold them — and the reason to roll forward
-- rather than back once a second phone is in use.
CREATE TABLE IF NOT EXISTS mobile_device (
    id               BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    hub_url          TEXT NOT NULL DEFAULT '',
    device_udid      TEXT NOT NULL DEFAULT '',
    platform_version TEXT NOT NULL DEFAULT '',
    device_addr      TEXT NOT NULL DEFAULT '',
    pin_encrypted    BYTEA,
    token_encrypted  BYTEA,
    last_connected_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO mobile_device (id, hub_url, device_udid, platform_version, device_addr,
                           pin_encrypted, token_encrypted, last_connected_at, updated_at)
SELECT TRUE, hub_url, device_udid, platform_version, device_addr,
       pin_encrypted, token_encrypted, last_connected_at, updated_at
FROM mobile_devices
ORDER BY created_at
LIMIT 1
ON CONFLICT (id) DO NOTHING;

DROP TABLE IF EXISTS mobile_devices;
