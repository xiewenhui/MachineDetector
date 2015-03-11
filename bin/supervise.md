#! /bin/sh

PATH=/usr/sbin:/sbin:/usr/bin:/bin
IFS=

cd `dirname "${0}"` || exit 1
readonly G_LOCAL_SBIN=`pwd`
ROOT_UID=0

run()
{
    while true; do
        ${G_LOCAL_SBIN}/machinedetector -D
        sleep 8
    done
    exit 1
}

if [ $# -eq 1 ]; then
    if [ x"${1}" = x"--run" ]; then
        run
    fi
fi

exec setsid "${0}" --run </dev/null >/dev/null 2>&1
exit 1
