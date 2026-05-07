#!/bin/sh
# ACP server entrypoint — auto-generates institution keys on first run and
# seeds a default policy snapshot on every startup.

set -e

KEYS_FILE=/data/institution-keys.env

if [ ! -f "$KEYS_FILE" ]; then
  echo "[acp-server] First run — generating institution Ed25519 keypair..."
  mkdir -p /data
  /keygen > "$KEYS_FILE"
  echo "[acp-server] Keys written to $KEYS_FILE (persisted on acp-data volume)"
fi

export ACP_INSTITUTION_PUBLIC_KEY
export ACP_INSTITUTION_PRIVATE_KEY
ACP_INSTITUTION_PUBLIC_KEY=$(grep ACP_INSTITUTION_PUBLIC_KEY  "$KEYS_FILE" | cut -d= -f2)
ACP_INSTITUTION_PRIVATE_KEY=$(grep ACP_INSTITUTION_PRIVATE_KEY "$KEYS_FILE" | cut -d= -f2)

# Start server in background so we can seed the policy snapshot
exec /acp-server &
ACP_PID=$!

# Wait for server to be ready then seed the default policy (idempotent)
echo "[acp-server] Waiting for server to be ready..."
for i in $(seq 1 20); do
  if wget -qO- http://localhost:8080/acp/v1/health > /dev/null 2>&1; then
    echo "[acp-server] Server ready — seeding default policy..."
    wget -qO- --post-data='{"approve_below":40,"escalate_below":70,"deny_at_or_above":70,"cooldown_trigger_after_denials":3,"cooldown_duration_seconds":300}' \
      --header='Content-Type: application/json' \
      http://localhost:8080/acp/v1/policy-snapshots > /dev/null 2>&1 && \
      echo "[acp-server] Default policy active (approve<40, escalate<70, deny>=70)"
    break
  fi
  sleep 0.5
done

wait $ACP_PID
