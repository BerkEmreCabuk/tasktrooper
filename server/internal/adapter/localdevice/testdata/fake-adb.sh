#!/bin/sh
# Stand-in for `adb` in the localdevice tests. See fake-xcrun.sh for why the
# fixtures are addressed through $LOCALDEVICE_FAKE_DIR.
#
#   adb-devices.txt    what `adb devices` prints
#   avd-<serial>.txt   what `adb -s <serial> emu avd name` answers
#   boot-completed     what getprop sys.boot_completed answers (default 1)
#   adb-argv.txt       every invocation, one line each
dir="${LOCALDEVICE_FAKE_DIR:-.}"
printf '%s\n' "$*" >> "$dir/adb-argv.txt"

if [ "$1" = "devices" ]; then
    if [ -f "$dir/adb-devices.txt" ]; then
        cat "$dir/adb-devices.txt"
    else
        printf 'List of devices attached\n'
    fi
    exit 0
fi

# Everything else is serial-scoped: adb -s <serial> <verb> ...
serial="$2"
shift 2

case "$1" in
    emu)
        case "$2" in
            avd)
                if [ -f "$dir/avd-$serial.txt" ]; then
                    cat "$dir/avd-$serial.txt"
                    printf 'OK\n'
                else
                    printf 'KO: unknown device\n'
                fi
                ;;
            kill)
                printf 'OK\n'
                ;;
        esac
        ;;
    wait-for-device)
        ;;
    shell)
        if [ "$3" = "sys.boot_completed" ]; then
            if [ -f "$dir/boot-completed" ]; then
                cat "$dir/boot-completed"
            else
                printf '1\n'
            fi
        fi
        ;;
esac
exit 0
