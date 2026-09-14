-- APNs push bildirimleri için kayıtlı cihaz token'ları. Tenant DB'si tek
-- kullanıcıya ait olduğundan kullanıcı kolonu yoktur; bir kullanıcının
-- birden çok cihazı olabilir.
CREATE TABLE IF NOT EXISTS push_devices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_token TEXT NOT NULL UNIQUE,
    platform     TEXT NOT NULL DEFAULT 'ios',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
