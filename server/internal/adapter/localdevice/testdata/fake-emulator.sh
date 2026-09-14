#!/bin/sh
# Stand-in for the Android SDK `emulator` binary. See fake-xcrun.sh for why the
# fixtures are addressed through $LOCALDEVICE_FAKE_DIR.
#
#   avds.txt        what `emulator -list-avds` prints
#   booting.txt     optional; when a `-avd` launch happens, its contents are
#                   APPENDED to adb-devices.txt so the poll in waitForSerial
#                   finds the device the way it would find a real one coming up
#   emulator-argv.txt   every invocation, one line each
dir="${LOCALDEVICE_FAKE_DIR:-.}"
printf '%s\n' "$*" >> "$dir/emulator-argv.txt"

if [ "$1" = "-list-avds" ]; then
    if [ -f "$dir/avds.txt" ]; then
        cat "$dir/avds.txt"
    fi
    exit 0
fi

if [ "$1" = "-avd" ] && [ -f "$dir/booting.txt" ]; then
    cat "$dir/booting.txt" >> "$dir/adb-devices.txt"
fi
exit 0
