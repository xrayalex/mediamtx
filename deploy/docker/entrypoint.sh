#!/bin/bash
# entrypoint for ip-cam/mediamtx:vms-*-analytics images.
#
# Starts the PlateCore license proxy (when configured) and mediamtx in
# parallel, then waits for either to exit. mediamtx exiting brings the
# container down with its exit code so docker can restart it; the
# license proxy exiting first is logged but ignored — LPR will then
# fail with PLATECORE_ERR_*, get downgraded to a warn line by the path
# logic, and the rest of the server keeps streaming.
#
# Requires bash 4.3+ for `wait -n`. Debian bookworm ships bash 5.

set -u

LICENSE_BIN="/opt/platecore/license-server-manager"

LICENSE_PID=""
MEDIAMTX_PID=""

cleanup() {
    if [[ -n "${MEDIAMTX_PID}" ]] && kill -0 "${MEDIAMTX_PID}" 2>/dev/null; then
        kill -TERM "${MEDIAMTX_PID}" 2>/dev/null || true
        wait "${MEDIAMTX_PID}" 2>/dev/null || true
    fi
    if [[ -n "${LICENSE_PID}" ]] && kill -0 "${LICENSE_PID}" 2>/dev/null; then
        kill -TERM "${LICENSE_PID}" 2>/dev/null || true
        wait "${LICENSE_PID}" 2>/dev/null || true
    fi
}

trap cleanup TERM INT

# License proxy ----------------------------------------------------------------
if [[ -n "${PLATECORE_LICENSE_CLUSTER_ADDRESS:-}" ]]; then
    PORT="${PLATECORE_LICENSE_CLUSTER_PORT:-9433}"
    echo "entrypoint: starting license proxy (cluster ${PLATECORE_LICENSE_CLUSTER_ADDRESS}:${PORT})"
    "${LICENSE_BIN}" \
        -mode=proxy \
        -cluster-address="${PLATECORE_LICENSE_CLUSTER_ADDRESS}" \
        -cluster-port="${PORT}" \
        -web-enabled=false &
    LICENSE_PID=$!
else
    echo "entrypoint: PLATECORE_LICENSE_CLUSTER_ADDRESS unset — LPR disabled"
fi

# mediamtx ---------------------------------------------------------------------
echo "entrypoint: starting mediamtx"
/mediamtx "$@" &
MEDIAMTX_PID=$!

# Wait for either to exit. ----------------------------------------------------
# The license proxy exiting first is non-fatal: PlateCore inits will
# return PLATECORE_ERR_* which the analytics layer logs as a warning
# and the corresponding path stays alive without LPR. We loop on
# `wait -n` so we keep watching mediamtx after losing the proxy.
while true; do
    if [[ -z "${MEDIAMTX_PID}" ]] || ! kill -0 "${MEDIAMTX_PID}" 2>/dev/null; then
        # mediamtx already gone; nothing useful to wait for.
        break
    fi

    if [[ -n "${LICENSE_PID}" ]]; then
        wait -n "${MEDIAMTX_PID}" "${LICENSE_PID}"
    else
        wait -n "${MEDIAMTX_PID}"
    fi

    if ! kill -0 "${MEDIAMTX_PID}" 2>/dev/null; then
        wait "${MEDIAMTX_PID}"
        EXIT_CODE=$?
        cleanup
        exit "${EXIT_CODE}"
    fi

    if [[ -n "${LICENSE_PID}" ]] && ! kill -0 "${LICENSE_PID}" 2>/dev/null; then
        echo "entrypoint: license proxy exited unexpectedly — LPR disabled, mediamtx continues"
        wait "${LICENSE_PID}" 2>/dev/null || true
        LICENSE_PID=""
    fi
done

wait "${MEDIAMTX_PID}"
EXIT_CODE=$?
cleanup
exit "${EXIT_CODE}"
