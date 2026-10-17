#!/bin/sh
set -eu

: "${ADMIN_URL:=http://localhost:8080}"

curl --fail-with-body --request POST "$ADMIN_URL/admin/shipment-events" \
  --header 'Content-Type: application/json' \
  --data '{
    "event_id": "evt-2026-0042",
    "shipment_id": "PKG-42",
    "domain": "changba.com",
    "status": "delivered",
    "proof": {
      "object_key": "proofs/PKG-42.pdf",
      "sha256": "71d7b5c46c531e8ec7c3f7434c3c9f1185b4776f6abda20e8810d2d72a1acb7e"
    }
  }'
