-- The mobile test device, registered from the settings UI instead of from env
-- vars and a kubectl exec.
--
-- 096 shipped the device tools reading tools.mobile out of config.yml, which
-- meant attaching a phone was: edit a secret, restart the tenant, then exec
-- into the Appium pod to run `adb pair` by hand. Every one of those steps is
-- an operator doing something the product could do — and the pairing code is
-- six digits that expire, so the one step that genuinely has to happen while
-- looking at the phone was also the one furthest from the person holding it.
--
-- One row, enforced by the primary key: there is one shared phone per
-- installation, and the lease design in 096 assumes exactly that. A second row
-- would silently mean two devices with one queue.
CREATE TABLE IF NOT EXISTS mobile_device (
    id               BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    -- Where Appium answers, and which phone it should drive.
    hub_url          TEXT NOT NULL DEFAULT '',
    device_udid      TEXT NOT NULL DEFAULT '',
    platform_version TEXT NOT NULL DEFAULT '',
    -- The adb endpoint the bridge connects to (host:port over the tailnet).
    -- Usually identical to device_udid — wireless adb names a device by its
    -- address — but kept separate because a cabled device has a serial UDID
    -- and no address at all.
    device_addr      TEXT NOT NULL DEFAULT '',
    -- Credentials. BYTEA and encrypted with MCP_SECRETS_KEY, the same
    -- treatment app_settings gives github_token: a lock-screen PIN unlocks a
    -- real phone, and the hub token is bearer auth for a service that can
    -- drive it.
    pin_encrypted    BYTEA,
    token_encrypted  BYTEA,
    -- Stamped by the last successful pair/connect, so the UI can say when the
    -- phone was last actually reachable rather than only what was typed in.
    last_connected_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
