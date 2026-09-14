-- Reverses 097. The device falls back to whatever tools.mobile carries in the
-- environment; an installation that only ever registered its phone from the UI
-- loses the registration, which is the honest outcome of removing the table it
-- lived in.
DROP TABLE IF EXISTS mobile_device;
