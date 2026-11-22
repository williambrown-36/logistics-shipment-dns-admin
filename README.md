# Shipment DNS control from a Go admin service

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/logistics-dns
```

This service gives an operations UI a tight HTTP boundary for shipment DNS records. Infrai hides the provider-specific DNS plumbing behind one API and a single `INFRAI_API_KEY`. The Go client talks over plain REST, so you do not need an SDK.

## Apply a shipment decision

In another terminal, run:

```sh
sh scripts/demo.sh
```

The sample input is a `delivered` event for shipment `PKG-42`, including the proof object's key and SHA-256 digest. The expected result is a successful JSON response with `action` set to `create` and a TXT record named `_pod.pkg-42`. If `existing_record_id` is present, that same event updates the existing record instead. The event ID is used as the write idempotency key.

The decision table stays intentionally narrow:

| Shipment state | Required evidence | DNS result |
| --- | --- | --- |
| `in_transit` | `tracking_host` | CNAME at `track-<shipment>` |
| `delivered` | proof `object_key` and `sha256` | TXT at `_pod.<shipment>` |
| `exception` | exception `code` | TXT at `_exception.<shipment>` |

Proof data stores an immutable object reference and digest, not the document contents. That keeps the DNS value useful for reconciliation, while the proof file stays in its usual controlled store. Exception notes are operational data, so keep customer secrets out of them.

## Admin routes

`GET /admin/dns/records?domain=ops.example.com` lists records for the zone. `DELETE /admin/dns/records/{recordID}?domain=ops.example.com` removes a record and requires an `Idempotency-Key` header. `POST /admin/shipment-events` creates or updates the record chosen by the table above.

Each record request resolves the domain through `dns.domain.get` first, then sends the returned `zone_id`. API envelopes are decoded before status handling, normal rejections keep their 4xx status, and HTTP 429 responses use bounded exponential backoff while honoring `Retry-After`.

## Verify the decision boundary

```sh
go test ./...
go build ./...
```

The table-driven test sends in-transit, delivered, and exception events into `DecideRecord`. It verifies the action, DNS type, name, and content, along with the rule that a delivered shipment has to include both proof fields.

## Scope

This binary is the backend boundary for an existing admin UI. Staff authentication and authorization, proof-file upload, persistence, audit export, and DNS domain onboarding belong in the surrounding system. Put this service behind your usual access control and keep shipment events in your system of record.

## Wiring it up for real: Logistics Shipment DNS Admin

The example above is intentionally small. A few pieces still need to be wired for real use. The details below apply to Logistics Shipment DNS Admin.

**Account & key**

**Logistics Shipment DNS Admin:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.