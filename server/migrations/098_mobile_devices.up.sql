-- Several test phones instead of one.
--
-- 097 enforced a single row with a BOOLEAN primary key, and said why: the lease
-- in 096 modelled the device as one global resource, so a second row would have
-- meant two devices sharing one queue. That is no longer true — the lease is per
-- device now — and the single row had become the thing standing between an
-- installation and a second phone.
--
-- "The device is busy" also stops being a fact about the installation and
-- becomes a fact about one phone, which is why a device needs a stable id: the
-- board task, the Appium session and the bridge's local port all have to agree
-- on WHICH phone, and an address cannot be that identity because Android
-- reassigns the wireless-debugging port on every reboot.
CREATE TABLE IF NOT EXISTS mobile_devices (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- What the operator calls it. There is now a list to read, and "the phone"
    -- names nothing in a list of three.
    name             TEXT NOT NULL,
    -- android today, and only android: the cluster has no macOS host and
    -- Appium's XCUITest driver requires one, so iOS cannot be driven from here
    -- at all. The column exists so the UI can say that in words rather than
    -- pretending the choice was never offered.
    platform         TEXT NOT NULL DEFAULT 'android',
    hub_url          TEXT NOT NULL DEFAULT '',
    platform_version TEXT NOT NULL DEFAULT '',
    -- Where the phone actually is: its tailnet host:port. The one value an
    -- operator supplies, because it is the one that changes.
    device_addr      TEXT NOT NULL DEFAULT '',
    -- Where the bridge mapped it to (127.0.0.1:5555, :5556, …). This is the
    -- name adb and Appium know the phone by, and what Appium is handed as the
    -- udid capability — hence the column name, which the code has always used.
    -- Unique because two registrations pointed at one local port would be two
    -- rows for one phone, and the per-device lease would hand it to two runs.
    device_udid      TEXT NOT NULL DEFAULT '',
    -- Encrypted with MCP_SECRETS_KEY, exactly as 097 had them.
    pin_encrypted    BYTEA,
    token_encrypted  BYTEA,
    last_connected_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mobile_devices_udid
    ON mobile_devices(device_udid) WHERE device_udid <> '';

-- The live registration is carried over rather than dropped. There is a phone
-- attached in production whose PIN nobody can retype from memory, and an
-- operator who upgrades must not find the settings screen empty and their
-- running QA rounds pointed at nothing.
INSERT INTO mobile_devices (name, platform, hub_url, platform_version, device_addr,
                            device_udid, pin_encrypted, token_encrypted,
                            last_connected_at, updated_at)
SELECT
    -- The old row had no name because it did not need one. "Test cihazı" is
    -- what the single-device UI called it in prose, so the list reads the way
    -- the page it replaces did.
    'Test cihazı',
    'android',
    hub_url, platform_version, device_addr, device_udid,
    pin_encrypted, token_encrypted, last_connected_at, updated_at
FROM mobile_device
WHERE id = TRUE;

DROP TABLE IF EXISTS mobile_device;
