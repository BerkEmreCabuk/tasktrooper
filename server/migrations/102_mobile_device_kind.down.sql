-- Dropping the column leaves every row reading as remote_adb again, which is
-- right for the bridge phones and wrong for any simulator or emulator that was
-- registered in the meantime. Those rows have to go with it: a simulator UDID
-- in a row the old code will hand to `adb connect` is a device that can never
-- come online and an operator staring at a red dot with no explanation.
DELETE FROM mobile_devices WHERE kind <> 'remote_adb';

ALTER TABLE mobile_devices DROP COLUMN IF EXISTS kind;
